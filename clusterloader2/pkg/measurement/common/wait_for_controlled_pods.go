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
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
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

	// In this measurement, we rely on the fact that handlers are being
	// processed in order - in particular gather() is checking if all
	// objects up to a given resource version has already been processed.
	// To guarantee processing order we can't have more than a single
	// worker. Fortunately it doesn't change much, because almost all
	// handler function is happening under lock.
	waitForControlledPodsWorkers = 1
)

var podIndexerFactory = &sharedPodIndexerFactory{}

func init() {
	if err := measurement.Register(waitForControlledPodsRunningName, createWaitForControlledPodsRunningMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForControlledPodsRunningName, err)
	}
}

type sharedPodIndexerFactory struct {
	podsIndexer *measurementutil.ControlledPodsIndexer
	err         error
	once        sync.Once
}

func (s *sharedPodIndexerFactory) PodsIndexer(c clientset.Interface) (*measurementutil.ControlledPodsIndexer, error) {
	s.once.Do(func() {
		s.podsIndexer, s.err = s.start(c)
	})
	return s.podsIndexer, s.err
}

func (s *sharedPodIndexerFactory) start(c clientset.Interface) (*measurementutil.ControlledPodsIndexer, error) {
	ctx := context.Background()
	informerFactory := informers.NewSharedInformerFactory(c, 0)
	podsIndexer, err := measurementutil.NewControlledPodsIndexer(
		informerFactory.Core().V1().Pods(),
		informerFactory.Apps().V1().ReplicaSets(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize controlledPodsIndexer: %w", err)
	}
	informerFactory.Start(ctx.Done())
	if !podsIndexer.WaitForCacheSync(ctx) {
		return nil, fmt.Errorf("failed to sync informers")
	}
	return podsIndexer, nil
}

func createWaitForControlledPodsRunningMeasurement() measurement.Measurement {
	return &waitForControlledPodsRunningMeasurement{
		selector:   util.NewObjectSelector(),
		queue:      workerqueue.NewWorkerQueue(waitForControlledPodsWorkers),
		objectKeys: sets.NewString(),
		checkerMap: checker.NewMap(),
	}
}

type waitForControlledPodsRunningMeasurement struct {
	apiVersion       string
	kind             string
	selector         *util.ObjectSelector
	operationTimeout time.Duration
	// countErrorMargin orders measurement to wait for number of pods to be in
	// <desired count - countErrorMargin, desired count> range
	// When using preemptibles on large scale, number of ready nodes is not stable
	// and reaching DesiredPodCount could take a very long time.
	countErrorMargin      int
	stopCh                chan struct{}
	isRunning             bool
	queue                 workerqueue.Interface
	handlingGroup         wait.Group
	lock                  sync.Mutex
	objectKeys            sets.String
	opResourceVersion     uint64
	gvr                   schema.GroupVersionResource
	checkerMap            checker.Map
	clusterFramework      *framework.Framework
	checkIfPodsAreUpdated bool
	// podsIndexer is an indexer propagated via informers observing
	// changes of all pods in the whole cluster.
	podsIndexer *measurementutil.ControlledPodsIndexer
}

// Execute waits until all specified controlling objects have all pods running or until timeout happens.
// Controlling objects can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
// "Start" action starts observation of the controlling objects, while "gather" waits for until
// specified number of controlling objects have all pods running.
func (w *waitForControlledPodsRunningMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
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
		w.countErrorMargin, err = util.GetIntOrDefault(config.Params, "countErrorMargin", 0)
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
	case "stop":
		w.Dispose()
		return nil, nil
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
		klog.V(2).Infof("%v: wait for controlled pods measurement already running", w)
		return nil
	}
	klog.V(2).Infof("%v: starting wait for controlled pods measurement...", w)
	gv, err := schema.ParseGroupVersion(w.apiVersion)
	if err != nil {
		return err
	}
	gvk := gv.WithKind(w.kind)
	w.gvr, _ = meta.UnsafeGuessKindToResource(gvk)

	w.isRunning = true
	w.stopCh = make(chan struct{})
	podsIndexer, err := podIndexerFactory.PodsIndexer(w.clusterFramework.GetClientSets().GetClient())
	if err != nil {
		return err
	}
	w.podsIndexer = podsIndexer

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
	klog.V(2).Infof("%v: waiting for controlled pods measurement...", w)
	if !w.isRunning {
		return fmt.Errorf("metric %s has not been started", w)
	}
	objectKeys, maxResourceVersion, err := w.getObjectKeysAndMaxVersion()
	if err != nil {
		return err
	}

	// Wait until checkers for all objects are registered:
	// - when object is created/updated, it's enough to wait for its resourceVersion to
	//   be processed by our handler; thus we wait until all events up to maxResourceVersion
	//   are processed before proceeding
	// - when object is deleted, by definition it will not be returned by the LIST request,
	//   thus resourceVersion of the deletion may be higher than the maxResourceVersion;
	//   we solve that by waiting until list of currently existing objects (that we propagate
	//   via our handler) is equal to the expected one;
	//   NOTE: we're not resiliant to situations where an object will be created/deleted
	//   after the LIST call happened. But given measurement and phases don't infer with
	//   each other, it can't be clusterloader that deleted it. Thus we accept this limitation.
	//   NOTE: we could try waiting for the informer state to be the same and use the
	//   resourceVersion from there, but then existence of bookmarks and the fact that our
	//   informer doesn't necessary follow all objects of a given type can break that.
	//   See #1259 for more details.

	cond := func() (bool, error) {
		w.lock.Lock()
		defer w.lock.Unlock()
		return w.opResourceVersion >= maxResourceVersion && objectKeys.Equal(w.objectKeys), nil
	}
	if err := wait.Poll(checkControlledPodsInterval, syncTimeout, cond); err != nil {
		return fmt.Errorf("timed out while waiting for controlled pods: %v", err)
	}

	w.handlingGroup.Wait()
	w.lock.Lock()
	defer w.lock.Unlock()
	var numberRunning, numberDeleted, numberTimeout, numberFailed int
	failedErrList := errors.NewErrorList()
	timedOutObjects := []string{}
	var maxDuration time.Duration
	for _, checker := range w.checkerMap {
		objChecker := checker.(*objectChecker)
		status, err := objChecker.getStatus()
		if objChecker.duration > maxDuration {
			maxDuration = objChecker.duration
		}
		switch status {
		case running:
			numberRunning++
		case deleted:
			numberDeleted++
		case timeout:
			timedOutObjects = append(timedOutObjects, objChecker.key)
			numberTimeout++
		case deleteTimeout:
			timedOutObjects = append(timedOutObjects, objChecker.key)
			numberTimeout++
			podsClient := w.clusterFramework.GetClientSets().GetClient().CoreV1().Pods(w.selector.Namespace)
			err := podsClient.DeleteCollection(context.Background(), forceDeleteOptions(), w.listOptions())
			if err != nil {
				klog.Errorf("Error: %s while Force Deleting Pod, %s", err, objChecker.key)
			}
		case failed:
			numberFailed++
			if err != nil {
				failedErrList.Append(err)
			}
		default:
			// Probably implementation bug.
			return fmt.Errorf("got unknown status for %v: status=%v, err=%v", objChecker.key, status, err)
		}
	}
	klog.V(2).Infof("%s: running %d, deleted %d, timeout: %d, failed: %d", w, numberRunning, numberDeleted, numberTimeout, numberFailed)
	var ratio float64
	if w.operationTimeout != 0 {
		ratio = float64(maxDuration) / float64(w.operationTimeout)
	}
	klog.V(2).Infof("%s: maxDuration=%v, operationTimeout=%v, ratio=%.2f", w, maxDuration, w.operationTimeout, ratio)
	if numberTimeout > 0 {
		klog.Errorf("Timed out %ss: %s", w.kind, strings.Join(timedOutObjects, ", "))
		return fmt.Errorf("%d objects timed out: %ss: %s", numberTimeout, w.kind, strings.Join(timedOutObjects, ", "))
	}
	if objectKeys.Len() != numberRunning {
		klog.Errorf("%s: incorrect objects number: %d/%d %ss are running with all pods", w, numberRunning, objectKeys.Len(), w.kind)
		return fmt.Errorf("incorrect objects number: %d/%d %ss are running with all pods", numberRunning, objectKeys.Len(), w.kind)
	}
	if numberFailed > 0 {
		klog.Errorf("%s: failed status for %d %ss: %s", w, numberFailed, w.kind, failedErrList.String())
		return fmt.Errorf("failed objects statuses: %v", failedErrList.String())
	}

	klog.V(2).Infof("%s: %d/%d %ss are running with all pods", w, numberRunning, objectKeys.Len(), w.kind)
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) listOptions() metav1.ListOptions {
	listOptions := metav1.ListOptions{
		LabelSelector: w.selector.LabelSelector,
		FieldSelector: w.selector.FieldSelector,
	}
	return listOptions
}

func forceDeleteOptions() metav1.DeleteOptions {
	gracePeriod := int64(0)
	propagationPolicy := metav1.DeletePropagationBackground
	forceDeletePodOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}
	return forceDeletePodOptions
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

	// Acquire the lock before defining defered function to ensure it
	// will be called under the same lock.
	w.lock.Lock()
	defer w.lock.Unlock()

	defer func() {
		if err := w.updateCacheLocked(oldRuntimeObj, newRuntimeObj); err != nil {
			klog.Errorf("%s: error when updating cache: %v", w, err)
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
	oldReplicasWatcher, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), oldObj)
	if err != nil {
		return false, err
	}
	oldReplicas, err := runtimeobjects.GetReplicasOnce(oldReplicasWatcher)
	if err != nil {
		return false, err
	}
	newReplicasWatcher, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), newObj)
	if err != nil {
		return false, err
	}
	newReplicas, err := runtimeobjects.GetReplicasOnce(newReplicasWatcher)
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
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(handledObj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}

	operationTimeout := w.operationTimeout
	// exactOperationTimeout controls whether we should skip multiplying by two operationTimeout on scale down/deletion.
	// Defaults to false for backward compatibility.
	// TODO(mborsz): Change default to true and remove.
	_, exactOperationTimeout := os.LookupEnv("CL2_WAIT_FOR_CONTROLLED_PODS_USE_EXACT_OPERATION_TIMEOUT")
	if !exactOperationTimeout && (isObjDeleted || isScalingDown) {
		// In case of deleting pods, twice as much time is required.
		// The pod deletion throughput equals half of the pod creation throughput.
		// NOTE: Starting from k8s 1.23 it's not true anymore, at least not in all cases.
		// TODO(mborsz): Can we remove this?
		operationTimeout *= 2
	}

	checker, err := w.waitForRuntimeObject(handledObj, isObjDeleted, operationTimeout)
	if err != nil {
		return fmt.Errorf("waiting for %v error: %v", key, err)
	}
	w.checkerMap.Add(key, checker)
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) deleteObjectLocked(obj runtime.Object) error {
	if obj == nil {
		return nil
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return fmt.Errorf("meta key creation error: %v", err)
	}
	w.checkerMap.DeleteAndStop(key)
	return nil
}

func (w *waitForControlledPodsRunningMeasurement) updateCacheLocked(oldObj, newObj runtime.Object) error {
	errList := errors.NewErrorList()

	if oldObj != nil {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(oldObj)
		if err != nil {
			errList.Append(fmt.Errorf("%s: retrieving key error: %v", w, err))
		} else {
			w.objectKeys.Delete(key)
		}
		if err := w.updateOpResourceVersionLocked(oldObj); err != nil {
			errList.Append(err)
		}
	}
	if newObj != nil {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
		if err != nil {
			errList.Append(fmt.Errorf("%s: retrieving key error: %v", w, err))
		} else {
			w.objectKeys.Insert(key)
		}
		if err := w.updateOpResourceVersionLocked(newObj); err != nil {
			errList.Append(err)
		}
	}

	if errList.IsEmpty() {
		return nil
	}
	return fmt.Errorf(errList.Error())
}

func (w *waitForControlledPodsRunningMeasurement) updateOpResourceVersionLocked(runtimeObj runtime.Object) error {
	version, err := runtimeobjects.GetResourceVersionFromRuntimeObject(runtimeObj)
	if err != nil {
		return fmt.Errorf("retriving resource version error: %v", err)
	}
	if version > w.opResourceVersion {
		w.opResourceVersion = version
	}
	return nil
}

// getObjectKeysAndMaxVersion returns keys of objects that satisfy measurement parameters
// and the maximal resource version of these objects.
func (w *waitForControlledPodsRunningMeasurement) getObjectKeysAndMaxVersion() (sets.String, uint64, error) {
	objects, err := runtimeobjects.ListRuntimeObjectsForKind(
		w.clusterFramework.GetDynamicClients().GetClient(),
		w.gvr, w.kind, w.selector.Namespace, w.selector.LabelSelector, w.selector.FieldSelector)
	if err != nil {
		return nil, 0, fmt.Errorf("listing objects error: %v", err)
	}

	objectKeys := sets.NewString()
	var maxResourceVersion uint64
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
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(runtimeObj)
		if err != nil {
			klog.Errorf("%s: retrieving key error: %v", w, err)
			continue
		}
		objectKeys.Insert(key)
		if version > maxResourceVersion {
			maxResourceVersion = version
		}
	}
	return objectKeys, maxResourceVersion, nil
}

func (w *waitForControlledPodsRunningMeasurement) waitForRuntimeObject(obj runtime.Object, isDeleted bool, operationTimeout time.Duration) (*objectChecker, error) {
	ctx := context.TODO()

	runtimeObjectReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(w.clusterFramework.GetClientSets().GetClient(), obj)
	if err != nil {
		return nil, err
	}
	var isPodUpdated func(*v1.Pod) error
	if w.checkIfPodsAreUpdated {
		isPodUpdated, err = runtimeobjects.GetIsPodUpdatedPredicateFromRuntimeObject(obj)
		if err != nil {
			return nil, err
		}
	}
	if isDeleted {
		runtimeObjectReplicas = &runtimeobjects.ConstReplicas{0}
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, fmt.Errorf("meta key creation error: %v", err)
	}

	podStore, err := measurementutil.NewOwnerReferenceBasedPodStore(w.podsIndexer, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to create pod store: %w", err)
	}

	o := newObjectChecker(key)
	o.lock.Lock()
	defer o.lock.Unlock()
	w.handlingGroup.Start(func() {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		o.SetCancel(cancel)
		if operationTimeout != time.Duration(0) {
			ctx, cancel = context.WithTimeout(ctx, operationTimeout)
			defer cancel()
		}
		if err := runtimeObjectReplicas.Start(ctx.Done()); err != nil {
			klog.Errorf("%s: error while starting runtimeObjectReplicas: %v", key, err)
			o.err = fmt.Errorf("failed to start runtimeObjectReplicas: %v", err)
			return
		}
		options := &measurementutil.WaitForPodOptions{
			DesiredPodCount:     runtimeObjectReplicas.Replicas,
			CountErrorMargin:    w.countErrorMargin,
			CallerName:          w.String(),
			WaitForPodsInterval: defaultWaitForPodsInterval,
			IsPodUpdated:        isPodUpdated,
		}

		// This function sets the status (and error message) for the object checker.
		// The handling of bad statuses and errors is done by gather() function of the measurement.
		start := time.Now()
		err := measurementutil.WaitForPods(ctx, podStore, options)
		o.lock.Lock()
		defer o.lock.Unlock()
		o.duration = time.Since(start)

		if err != nil {
			klog.Errorf("%s: error for %v: %v", w, key, err)
			o.status = failed
			o.err = fmt.Errorf("%s: %v", key, err)

			hasTimedOut := ctx.Err() != nil
			if hasTimedOut {
				if isDeleted {
					o.status = deleteTimeout
				} else {
					o.status = timeout
				}
				klog.Errorf("%s: %s timed out", w, key)
			}
			return
		}
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
	unknown       objectStatus = iota // WaitForPods hasn't finished yet. Result isn't determined yet.
	running                           // WaitForPods finished and scale up/down to scale other than 0 succeeded.
	deleted                           // WaitForPods finished and scale down to 0 succeeded.
	failed                            // WaitForPods finished, but failed. o.err must be set.
	timeout                           // WaitForPods has been interrupted due to timeout and target scale was other than 0.
	deleteTimeout                     // WaitForPods has been interrupted due to timeout and target scale was 0.
)

type objectChecker struct {
	lock   sync.Mutex
	status objectStatus
	err    error
	// key of the object being checked. In the current implementation it's a namespaced name, but it
	// may change in the future.
	key      string
	cancel   context.CancelFunc
	duration time.Duration
}

func newObjectChecker(key string) *objectChecker {
	return &objectChecker{
		status: unknown,
		key:    key,
	}
}

func (o *objectChecker) SetCancel(cancel context.CancelFunc) {
	o.lock.Lock()
	defer o.lock.Unlock()
	o.cancel = cancel
}

func (o *objectChecker) Stop() {
	o.lock.Lock()
	defer o.lock.Unlock()
	o.cancel()
}

func (o *objectChecker) getStatus() (objectStatus, error) {
	o.lock.Lock()
	defer o.lock.Unlock()
	return o.status, o.err
}
