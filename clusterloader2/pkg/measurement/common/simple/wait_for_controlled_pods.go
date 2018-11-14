/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
TODO(krzysied): Replace ClientSet interface with dynamic client.
This will allow more generic approach.
*/

package simple

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultSyncTimeout               = 60 * time.Second
	defaultOperationTimeout          = 10 * time.Minute
	checkControlledPodsInterval      = 5 * time.Second
	informerSyncTimeout              = time.Minute
	waitForControlledPodsRunningName = "WaitForControlledPodsRunning"
	waitForControlledPodsWorkers     = 10
)

func init() {
	measurement.Register(waitForControlledPodsRunningName, createWaitForControlledPodsRunningMeasurement)
}

func createWaitForControlledPodsRunningMeasurement() measurement.Measurement {
	return &waitForControlledPodsRunningMeasurement{
		queue:      workqueue.New(),
		checkerMap: make(map[string]*objectChecker),
	}
}

type waitForControlledPodsRunningMeasurement struct {
	informer          cache.SharedInformer
	kind              string
	namespace         string
	labelSelector     string
	fieldSelector     string
	operationTimeout  time.Duration
	stopCh            chan struct{}
	isRunning         bool
	queue             workqueue.Interface
	workerGroup       wait.Group
	handlingGroup     wait.Group
	lock              sync.Mutex
	opResourceVersion uint64
	checkerMap        map[string]*objectChecker
}

// Execute waits until all specified controlling objects have all pods running or until timeout happens.
// Controlling objects can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
// "Start" action starts observation of the controlling objects, while "gather" waits for until
// specified number of controlling objects have all pods running.
func (w *waitForControlledPodsRunningMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}

	switch action {
	case "start":
		w.kind, err = util.GetString(config.Params, "kind")
		if err != nil {
			return summaries, err
		}
		w.namespace, err = util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
		if err != nil {
			return summaries, err
		}
		w.labelSelector, err = util.GetStringOrDefault(config.Params, "labelSelector", "")
		if err != nil {
			return summaries, err
		}
		w.fieldSelector, err = util.GetStringOrDefault(config.Params, "fieldSelector", "")
		if err != nil {
			return summaries, err
		}
		w.operationTimeout, err = util.GetDurationOrDefault(config.Params, "operationTimeout", defaultOperationTimeout)
		if err != nil {
			return summaries, err
		}
		return summaries, w.start(config.ClientSet)
	case "gather":
		syncTimeout, err := util.GetDurationOrDefault(config.Params, "syncTimeout", defaultSyncTimeout)
		if err != nil {
			return summaries, err
		}
		return summaries, w.gather(config.ClientSet, syncTimeout)
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (w *waitForControlledPodsRunningMeasurement) Dispose() {
	close(w.stopCh)
	w.queue.ShutDown()
	w.workerGroup.Wait()
	w.lock.Lock()
	defer w.lock.Unlock()
	w.isRunning = false
	for _, checker := range w.checkerMap {
		checker.terminate()
	}
}

// String returns a string representation of the metric.
func (*waitForControlledPodsRunningMeasurement) String() string {
	return waitForControlledPodsRunningName
}

func (w *waitForControlledPodsRunningMeasurement) start(c clientset.Interface) error {
	if w.informer != nil {
		glog.Infof("%v: wait for controlled pods measurement already running", w)
		return nil
	}
	glog.Infof("%v: starting wait for controlled pods measurement...", w)
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = w.fieldSelector
		options.LabelSelector = w.labelSelector
	}
	listerWatcher := cache.NewFilteredListWatchFromClient(c.CoreV1().RESTClient(), w.kind+"s", w.namespace, optionsModifier)
	w.isRunning = true
	w.stopCh = make(chan struct{})
	w.informer = cache.NewSharedInformer(listerWatcher, nil, 0)
	w.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addF := func() {
				w.handleObject(c, nil, obj)
			}
			w.queue.Add(&addF)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			updateF := func() {
				w.handleObject(c, oldObj, newObj)
			}
			w.queue.Add(&updateF)
		},
		DeleteFunc: func(obj interface{}) {
			deleteF := func() {
				w.handleObject(c, obj, nil)
			}
			w.queue.Add(&deleteF)
		},
	})

	for i := 0; i < waitForControlledPodsWorkers; i++ {
		w.workerGroup.Start(w.worker)
	}
	go w.informer.Run(w.stopCh)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerSyncTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, w.informer.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}

	return nil
}

func (w *waitForControlledPodsRunningMeasurement) worker() {
	for {
		f, stop := w.queue.Get()
		if stop {
			return
		}
		(*f.(*func()))()
		w.queue.Done(f)
	}
}

func (w *waitForControlledPodsRunningMeasurement) gather(c clientset.Interface, syncTimeout time.Duration) error {
	glog.Infof("%v: waiting for controlled pods measurement...", w)
	if w.informer == nil {
		return fmt.Errorf("metric %s has not been started", w)
	}
	desiredCount, maxResourceVersion, err := w.getObjectCountAndMaxVersion(c)
	if err != nil {
		return err
	}

	cond := func() (bool, error) {
		return w.opResourceVersion >= maxResourceVersion, nil
	}
	if err := wait.Poll(checkControlledPodsInterval, syncTimeout, cond); err != nil {
		return fmt.Errorf("timed out while waiting for controlled pods")
	}

	w.handlingGroup.Wait()
	w.lock.Lock()
	defer w.lock.Unlock()
	numberRunning := 0
	for _, checker := range w.checkerMap {
		if status := checker.getStatus(); status {
			numberRunning++
		}
	}
	if desiredCount != numberRunning {
		glog.Errorf("%s: incorrect objects number: %d/%d %ss are running with all pods", w, numberRunning, desiredCount, w.kind)
		return fmt.Errorf("incorrect objects number")
	}

	glog.Infof("%s: %d/%d %ss are running with all pods", w, numberRunning, desiredCount, w.kind)
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) handleObject(c clientset.Interface, oldObj, newObj interface{}) {
	var oldRuntimeObj runtime.Object
	var newRuntimeObj runtime.Object
	var ok bool
	oldRuntimeObj, ok = oldObj.(runtime.Object)
	if oldObj != nil && !ok {
		glog.Errorf("%s: uncastable old object: %v", w, oldObj)
		return
	}
	newRuntimeObj, ok = newObj.(runtime.Object)
	if newObj != nil && !ok {
		glog.Errorf("%s: uncastable new object: %v", w, newObj)
		return
	}
	defer func() {
		// We want to update version after (potentially) creating goroutine.
		if err := w.updateOpResourceVersion(oldRuntimeObj); err != nil {
			glog.Errorf("%s: updating resource version error: %v", w, err)
		}
		if err := w.updateOpResourceVersion(newRuntimeObj); err != nil {
			glog.Errorf("%s: updating resource version error: %v", w, err)
		}
	}()
	isEqual, err := runtimeobjects.IsEqualRuntimeObjectsSpec(oldRuntimeObj, newRuntimeObj)
	if err != nil {
		glog.Errorf("%s: comparing specs error: %v", w, err)
		return
	}
	if isEqual {
		// Skip updates without changes in the spec.
		return
	}

	w.lock.Lock()
	defer w.lock.Unlock()
	if !w.isRunning {
		return
	}

	if err := w.deleteObjectLocked(c, oldRuntimeObj); err != nil {
		glog.Errorf("%s: delete checker error: %v", w, err)
	} else if newRuntimeObj == nil {
		key, err := runtimeobjects.CreateMetaNamespaceKey(oldRuntimeObj)
		if err != nil {
			glog.Errorf("%s: meta key creation error: %v", w, err)
			return
		}
		glog.Infof("%s: %v has been deleted", w, key)
	}
	if err := w.createObjectLocked(c, newRuntimeObj); err != nil {
		glog.Errorf("%s: create checker error: %v", w, err)
	}
}

func (w *waitForControlledPodsRunningMeasurement) createObjectLocked(c clientset.Interface, obj runtime.Object) error {
	if obj == nil {
		return nil
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(obj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}
	checker, err := w.waitForRuntimeObject(c, obj)
	if err != nil {
		return fmt.Errorf("waiting for %v error: %v", key, err)
	}
	time.AfterFunc(w.operationTimeout, func() {
		checker.terminate()
	})
	w.checkerMap[key] = checker
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) deleteObjectLocked(c clientset.Interface, obj runtime.Object) error {
	if obj == nil {
		return nil
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(obj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}
	if checker, exists := w.checkerMap[key]; exists {
		checker.terminate()
		delete(w.checkerMap, key)
	}
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) updateOpResourceVersion(runtimeObj runtime.Object) error {
	if runtimeObj == nil {
		return nil
	}
	version, err := runtimeobjects.GetResourceVersionFromRuntimeObject(runtimeObj)
	if err != nil {
		return fmt.Errorf("retriving resource version error: %v", err)
	}
	w.lock.Lock()
	defer w.lock.Unlock()
	if version > w.opResourceVersion {
		w.opResourceVersion = version
	}
	return nil
}

// getObjectCountAndMaxVersion returns number of objects that satisfy measurements parameters
// and maximal resource version of these objects.
// These two values allow to properly handle all object operations:
// - When create/delete operation are called we expect the exact number of objects.
// - When objects is updated we expect to receive event referencing this specific version.
//   Using maximum from objects resource versions assures that all updates will be processed.
func (w *waitForControlledPodsRunningMeasurement) getObjectCountAndMaxVersion(c clientset.Interface) (int, uint64, error) {
	var desiredCount int
	var maxResourceVersion uint64
	objects, err := runtimeobjects.ListRuntimeObjectsForKind(c, w.kind, w.namespace, w.labelSelector, w.fieldSelector)
	if err != nil {
		return desiredCount, maxResourceVersion, fmt.Errorf("listing objects error: %v", err)
	}

	for i := range objects {
		runtimeObj, ok := objects[i].(runtime.Object)
		if !ok {
			glog.Errorf("%s: cannot cast to runtime.Object: %v", objects[i])
			continue
		}
		version, err := runtimeobjects.GetResourceVersionFromRuntimeObject(runtimeObj)
		if err != nil {
			glog.Errorf("%s: retriving resource version error: %v", w, err)
			continue
		}
		desiredCount++
		if version > maxResourceVersion {
			maxResourceVersion = version
		}
	}
	return desiredCount, maxResourceVersion, nil
}

func (w *waitForControlledPodsRunningMeasurement) waitForRuntimeObject(clientSet clientset.Interface, obj runtime.Object) (*objectChecker, error) {
	runtimeObjectNamespace, err := runtimeobjects.GetNamespaceFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	runtimeObjectSelector, err := runtimeobjects.GetSelectorFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	runtimeObjectReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(obj)
	if err != nil {
		return nil, fmt.Errorf("meta key creation error: %v", err)
	}

	o := newObjectCheker()
	o.lock.Lock()
	defer o.lock.Unlock()
	w.handlingGroup.Start(func() {
		err = waitForPods(clientSet, runtimeObjectNamespace, runtimeObjectSelector.String(), "", int(runtimeObjectReplicas), o.stopCh, false)
		o.lock.Lock()
		defer o.lock.Unlock()
		if err != nil {
			if o.isRunning {
				// Log error only if checker wasn't terminated.
				glog.Errorf("%s: error for %v: %v", w, key, err)
			}
			return
		}
		glog.Infof("%s: %v has all pods (%d) running", w, key, runtimeObjectReplicas)
		o.status = true
	})
	return o, nil
}

type objectChecker struct {
	lock      sync.Mutex
	isRunning bool
	stopCh    chan struct{}
	status    bool
}

func newObjectCheker() *objectChecker {
	return &objectChecker{
		stopCh:    make(chan struct{}),
		isRunning: true,
	}
}

func (o *objectChecker) getStatus() bool {
	o.lock.Lock()
	defer o.lock.Unlock()
	return o.status
}

func (o *objectChecker) terminate() {
	o.lock.Lock()
	defer o.lock.Unlock()
	if o.isRunning {
		close(o.stopCh)
		o.isRunning = false
	}
}
