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

package simple

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
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
	if err := measurement.Register(waitForControlledPodsRunningName, createWaitForControlledPodsRunningMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForControlledPodsRunningName, err)
	}
}

func createWaitForControlledPodsRunningMeasurement() measurement.Measurement {
	return &waitForControlledPodsRunningMeasurement{
		queue:      workqueue.New(),
		checkerMap: make(map[string]*objectChecker),
	}
}

type waitForControlledPodsRunningMeasurement struct {
	informer          cache.SharedInformer
	apiVersion        string
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
	gvr               schema.GroupVersionResource
	checkerMap        map[string]*objectChecker
	clusterFramework  *framework.Framework
}

// Execute waits until all specified controlling objects have all pods running or until timeout happens.
// Controlling objects can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
// "Start" action starts observation of the controlling objects, while "gather" waits for until
// specified number of controlling objects have all pods running.
func (w *waitForControlledPodsRunningMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	w.clusterFramework = config.ClusterFramework

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		w.apiVersion, err = util.GetString(config.Params, "apiVersion")
		if err != nil {
			return nil, err
		}
		w.kind, err = util.GetString(config.Params, "kind")
		if err != nil {
			return nil, err
		}
		w.namespace, err = util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
		if err != nil {
			return nil, err
		}
		w.labelSelector, err = util.GetStringOrDefault(config.Params, "labelSelector", "")
		if err != nil {
			return nil, err
		}
		w.fieldSelector, err = util.GetStringOrDefault(config.Params, "fieldSelector", "")
		if err != nil {
			return nil, err
		}
		w.operationTimeout, err = util.GetDurationOrDefault(config.Params, "operationTimeout", defaultOperationTimeout)
		if err != nil {
			return nil, err
		}
		return nil, w.start()
	case "gather":
		syncTimeout, err := util.GetDurationOrDefault(config.Params, "syncTimeout", defaultSyncTimeout)
		if err != nil {
			return nil, err
		}
		return nil, w.gather(syncTimeout)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
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
		checker.terminate(false)
	}
}

// String returns a string representation of the metric.
func (*waitForControlledPodsRunningMeasurement) String() string {
	return waitForControlledPodsRunningName
}

func (w *waitForControlledPodsRunningMeasurement) newInformer() (cache.SharedInformer, error) {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = w.fieldSelector
		options.LabelSelector = w.labelSelector
	}
	c := w.clusterFramework.GetDynamicClients()
	if c == nil {
		return nil, fmt.Errorf("no clientsets for measurement")
	}
	tweakListOptions := dynamicinformer.TweakListOptionsFunc(optionsModifier)
	dInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(c.GetClient(), 0, w.namespace, tweakListOptions)

	informer := dInformerFactory.ForResource(w.gvr).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addF := func() {
				w.handleObject(nil, obj)
			}
			w.queue.Add(&addF)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			updateF := func() {
				w.handleObject(oldObj, newObj)
			}
			w.queue.Add(&updateF)
		},
		DeleteFunc: func(obj interface{}) {
			deleteF := func() {
				if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					w.handleObject(tombstone.Obj, nil)
				} else {
					w.handleObject(obj, nil)
				}
			}
			w.queue.Add(&deleteF)
		},
	})

	return informer, nil
}
func (w *waitForControlledPodsRunningMeasurement) start() error {
	gv, err := schema.ParseGroupVersion(w.apiVersion)
	if err != nil {
		return err
	}
	gvk := gv.WithKind(w.kind)
	w.gvr, _ = meta.UnsafeGuessKindToResource(gvk)

	if w.informer != nil {
		klog.Infof("%v: wait for controlled pods measurement already running", w)
		return nil
	}
	klog.Infof("%v: starting wait for controlled pods measurement...", w)

	w.isRunning = true
	w.stopCh = make(chan struct{})

	w.informer, err = w.newInformer()
	if err != nil {
		klog.Infof("%v: create dynamic informer failed.", w)
		return err
	}

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

func (w *waitForControlledPodsRunningMeasurement) gather(syncTimeout time.Duration) error {
	klog.Infof("%v: waiting for controlled pods measurement...", w)
	if w.informer == nil {
		return fmt.Errorf("metric %s has not been started", w)
	}
	desiredCount, maxResourceVersion, err := w.getObjectCountAndMaxVersion()
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
	var numberRunning, numberDeleted, numberTimeout, numberUnknown int
	unknowStatusErrList := errors.NewErrorList()
	timedOutObjects := []string{}
	for _, checker := range w.checkerMap {
		status, err := checker.getStatus()
		switch status {
		case running:
			numberRunning++
		case deleted:
			numberDeleted++
		case timeout:
			timedOutObjects = append(timedOutObjects, checker.key)
			numberTimeout++
		default:
			numberUnknown++
			if err != nil {
				unknowStatusErrList.Append(err)
			}
		}
	}
	klog.Infof("%s: running %d, deleted %d, timeout: %d, unknown: %d", w, numberRunning, numberDeleted, numberTimeout, numberUnknown)
	if numberTimeout > 0 {
		klog.Errorf("Timed out %ss: %s", w.kind, strings.Join(timedOutObjects, ", "))
		return fmt.Errorf("%d objects timed out: %ss: %s", numberTimeout, w.kind, strings.Join(timedOutObjects, ", "))
	}
	if desiredCount != numberRunning {
		klog.Errorf("%s: incorrect objects number: %d/%d %ss are running with all pods", w, numberRunning, desiredCount, w.kind)
		return fmt.Errorf("incorrect objects number: %d/%d %ss are running with all pods", numberRunning, desiredCount, w.kind)
	}
	if numberUnknown > 0 {
		klog.Errorf("%s: unknown status for %d %ss: %s", w, numberUnknown, w.kind, unknowStatusErrList.String())
		return fmt.Errorf("unknown objects statuses: %v", unknowStatusErrList.String())
	}

	klog.Infof("%s: %d/%d %ss are running with all pods", w, numberRunning, desiredCount, w.kind)
	return nil
}

// handleObject manages checker for given controlling pod object.
// This function does not return errors only logs them. All possible errors will be caught in gather function.
// If this function does not executes correctly, verifying number of running pods will fail,
// causing incorrect objects number error to be returned.
func (w *waitForControlledPodsRunningMeasurement) handleObject(oldObj, newObj interface{}) {
	var oldRuntimeObj runtime.Object
	var newRuntimeObj runtime.Object
	var ok bool
	oldRuntimeObj, ok = oldObj.(runtime.Object)
	if oldObj != nil && !ok {
		klog.Errorf("%s: uncastable old object: %v", w, oldObj)
		return
	}
	newRuntimeObj, ok = newObj.(runtime.Object)
	if newObj != nil && !ok {
		klog.Errorf("%s: uncastable new object: %v", w, newObj)
		return
	}
	defer func() {
		// We want to update version after (potentially) creating goroutine.
		if err := w.updateOpResourceVersion(oldRuntimeObj); err != nil {
			klog.Errorf("%s: updating resource version error: %v", w, err)
		}
		if err := w.updateOpResourceVersion(newRuntimeObj); err != nil {
			klog.Errorf("%s: updating resource version error: %v", w, err)
		}
	}()
	isEqual, err := runtimeobjects.IsEqualRuntimeObjectsSpec(oldRuntimeObj, newRuntimeObj)
	if err != nil {
		klog.Errorf("%s: comparing specs error: %v", w, err)
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

	if err := w.deleteObjectLocked(oldRuntimeObj); err != nil {
		klog.Errorf("%s: delete checker error: %v", w, err)
	}
	if err := w.handleObjectLocked(oldRuntimeObj, newRuntimeObj); err != nil {
		klog.Errorf("%s: create checker error: %v", w, err)
	}
}

func checkScaledown(oldObj, newObj runtime.Object) (bool, error) {
	oldReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(oldObj)
	if err != nil {
		return false, err
	}
	newReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(newObj)
	if err != nil {
		return false, err
	}

	return newReplicas < oldReplicas, nil
}

func (w *waitForControlledPodsRunningMeasurement) handleObjectLocked(oldObj, newObj runtime.Object) error {
	isObjDeleted := newObj == nil
	isScalingDown, err := checkScaledown(oldObj, newObj)
	if err != nil {
		return fmt.Errorf("checkScaledown error: %v", err)
	}

	handledObj := newObj
	if isObjDeleted {
		handledObj = oldObj
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(handledObj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}
	checker, err := w.waitForRuntimeObject(handledObj, isObjDeleted)
	if err != nil {
		return fmt.Errorf("waiting for %v error: %v", key, err)
	}

	operationTimeout := w.operationTimeout
	if isObjDeleted || isScalingDown {
		// In case of deleting pods, twice as much time is required.
		// The pod deletion throughput equals half of the pod creation throughput.
		operationTimeout *= 2
	}
	time.AfterFunc(operationTimeout, func() {
		checker.terminate(true)
	})
	w.checkerMap[key] = checker
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) deleteObjectLocked(obj runtime.Object) error {
	if obj == nil {
		return nil
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(obj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}
	if checker, exists := w.checkerMap[key]; exists {
		checker.terminate(false)
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
func (w *waitForControlledPodsRunningMeasurement) getObjectCountAndMaxVersion() (int, uint64, error) {
	var desiredCount int
	var maxResourceVersion uint64
	objects, err := runtimeobjects.ListRuntimeObjectsForKind(w.clusterFramework.GetClientSets().GetClient(), w.kind, w.namespace, w.labelSelector, w.fieldSelector)
	if err != nil {
		return desiredCount, maxResourceVersion, fmt.Errorf("listing objects error: %v", err)
	}

	for i := range objects {
		runtimeObj, ok := objects[i].(runtime.Object)
		if !ok {
			klog.Errorf("%s: cannot cast to runtime.Object: %v", w, objects[i])
			continue
		}
		version, err := runtimeobjects.GetResourceVersionFromRuntimeObject(runtimeObj)
		if err != nil {
			klog.Errorf("%s: retriving resource version error: %v", w, err)
			continue
		}
		desiredCount++
		if version > maxResourceVersion {
			maxResourceVersion = version
		}
	}
	return desiredCount, maxResourceVersion, nil
}

func (w *waitForControlledPodsRunningMeasurement) waitForRuntimeObject(obj runtime.Object, isDeleted bool) (*objectChecker, error) {
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
	if isDeleted {
		runtimeObjectReplicas = 0
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(obj)
	if err != nil {
		return nil, fmt.Errorf("meta key creation error: %v", err)
	}

	o := newObjectChecker(key)
	o.lock.Lock()
	defer o.lock.Unlock()
	w.handlingGroup.Start(func() {
		// This function sets the status (and error message) for the object checker.
		// The handling of bad statuses and errors is done by gather() function of the measurement.
		err = waitForPods(w.clusterFramework.GetClientSets().GetClient(), runtimeObjectNamespace, runtimeObjectSelector.String(), "", int(runtimeObjectReplicas), o.stopCh, true, w.String())
		o.lock.Lock()
		defer o.lock.Unlock()
		if err != nil {
			if o.isRunning {
				// Log error only if checker wasn't terminated.
				klog.Errorf("%s: error for %v: %v", w, key, err)
				o.err = fmt.Errorf("%s: %v", key, err)
			}
			if o.status == timeout {
				klog.Errorf("%s: %s timed out", w, key)
			}
			return
		}
		o.isRunning = false
		if isDeleted {
			o.status = deleted
			return
		}

		o.status = running
	})
	return o, nil
}

type objectStatus int

const (
	unknown objectStatus = iota
	running
	deleted
	timeout
)

type objectChecker struct {
	lock      sync.Mutex
	isRunning bool
	stopCh    chan struct{}
	status    objectStatus
	err       error
	// key of the object being checked. In the current implementation it's a namespaced name, but it
	// may change in the future.
	key string
}

func newObjectChecker(key string) *objectChecker {
	return &objectChecker{
		stopCh:    make(chan struct{}),
		isRunning: true,
		status:    unknown,
		key:       key,
	}
}

func (o *objectChecker) getStatus() (objectStatus, error) {
	o.lock.Lock()
	defer o.lock.Unlock()
	return o.status, o.err
}

func (o *objectChecker) terminate(hasTimedOut bool) {
	o.lock.Lock()
	defer o.lock.Unlock()
	if o.isRunning {
		close(o.stopCh)
		o.isRunning = false
		if hasTimedOut {
			o.status = timeout
		}
	}
}
