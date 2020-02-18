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

package common

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/checker"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/workerqueue"
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
		selector:   measurementutil.NewObjectSelector(),
		queue:      workerqueue.NewWorkerQueue(waitForControlledPodsWorkers),
		checkerMap: checker.NewCheckerMap(),
	}
}

type waitForControlledPodsRunningMeasurement struct {
	apiVersion            string
	kind                  string
	selector              *measurementutil.ObjectSelector
	operationTimeout      time.Duration
	stopCh                chan struct{}
	isRunning             bool
	queue                 workerqueue.Interface
	handlingGroup         wait.Group
	lock                  sync.Mutex
	opResourceVersion     uint64
	gvr                   schema.GroupVersionResource
	checkerMap            checker.CheckerMap
	clusterFramework      *framework.Framework
	checkIfPodsAreUpdated bool
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
		if err = w.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		w.operationTimeout, err = util.GetDurationOrDefault(config.Params, "operationTimeout", defaultOperationTimeout)
		if err != nil {
			return nil, err
		}
		w.checkIfPodsAreUpdated, err = util.GetBoolOrDefault(config.Params, "checkIfPodsAreUpdated", true)
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
	if !w.isRunning {
		return
	}
	w.isRunning = false
	close(w.stopCh)
	w.queue.Stop()
	w.lock.Lock()
	defer w.lock.Unlock()
	w.checkerMap.Dispose()
}

// String returns a string representation of the metric.
func (*waitForControlledPodsRunningMeasurement) String() string {
	return waitForControlledPodsRunningName
}

func (w *waitForControlledPodsRunningMeasurement) start() error {
	if w.isRunning {
		klog.Infof("%v: wait for controlled pods measurement already running", w)
		return nil
	}
	klog.Infof("%v: starting wait for controlled pods measurement...", w)
	gv, err := schema.ParseGroupVersion(w.apiVersion)
	if err != nil {
		return err
	}
	gvk := gv.WithKind(w.kind)
	w.gvr, _ = meta.UnsafeGuessKindToResource(gvk)

	w.isRunning = true
	w.stopCh = make(chan struct{})
	i := informer.NewDynamicInformer(
		w.clusterFramework.GetDynamicClients().GetClient(),
		w.gvr,
		w.selector,
		func(odlObj, newObj interface{}) {
			f := func() {
				w.handleObject(odlObj, newObj)
			}
			w.queue.Add(&f)
		},
	)
	return informer.StartAndSync(i, w.stopCh, informerSyncTimeout)
}

func (w *waitForControlledPodsRunningMeasurement) gather(syncTimeout time.Duration) error {
	klog.Infof("%v: waiting for controlled pods measurement...", w)
	if !w.isRunning {
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
		objChecker := checker.(*objectChecker)
		status, err := objChecker.getStatus()
		switch status {
		case running:
			numberRunning++
		case deleted:
			numberDeleted++
		case timeout:
			timedOutObjects = append(timedOutObjects, objChecker.key)
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

func (w *waitForControlledPodsRunningMeasurement) checkScaledown(oldObj, newObj runtime.Object) (bool, error) {
	oldReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), oldObj)
	if err != nil {
		return false, err
	}
	newReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), newObj)
	if err != nil {
		return false, err
	}

	return newReplicas < oldReplicas, nil
}

func (w *waitForControlledPodsRunningMeasurement) handleObjectLocked(oldObj, newObj runtime.Object) error {
	isObjDeleted := newObj == nil
	isScalingDown, err := w.checkScaledown(oldObj, newObj)
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
	w.checkerMap.Add(key, checker)
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
	w.checkerMap.DeleteAndStop(key)
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
	objects, err := runtimeobjects.ListRuntimeObjectsForKind(
		w.clusterFramework.GetDynamicClients().GetClient(),
		w.gvr, w.kind, w.selector.Namespace, w.selector.LabelSelector, w.selector.FieldSelector)
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
	runtimeObjectReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), obj)
	if err != nil {
		return nil, err
	}
	var isPodUpdated func(*v1.Pod) bool
	if w.checkIfPodsAreUpdated {
		isPodUpdated, err = runtimeobjects.GetIsPodUpdatedPredicateFromRuntimeObject(obj)
		if err != nil {
			return nil, err
		}
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
		options := &measurementutil.WaitForPodOptions{
			Selector: &measurementutil.ObjectSelector{
				Namespace:     runtimeObjectNamespace,
				LabelSelector: runtimeObjectSelector.String(),
				FieldSelector: "",
			},
			DesiredPodCount:     int(runtimeObjectReplicas),
			EnableLogging:       true,
			CallerName:          w.String(),
			WaitForPodsInterval: defaultWaitForPodsInterval,
			IsPodUpdated:        isPodUpdated,
		}
		// This function sets the status (and error message) for the object checker.
		// The handling of bad statuses and errors is done by gather() function of the measurement.
		err = measurementutil.WaitForPods(w.clusterFramework.GetClientSets().GetClient(), o.stopCh, options)
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

func (o *objectChecker) Stop() {
	o.terminate(false)
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
