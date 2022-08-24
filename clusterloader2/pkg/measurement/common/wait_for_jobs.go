/*
Copyright 2022 The Kubernetes Authors.

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
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/workerqueue"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForFinishedJobsTimeout = 10 * time.Minute
	waitForFinishedJobsName           = "WaitForFinishedJobs"
	waitForFinishedJobsWorkers        = 1
	checkFinishedJobsInterval         = time.Second
)

func init() {
	if err := measurement.Register(waitForFinishedJobsName, createWaitForFinishedJobsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForFinishedJobsName, err)
	}
}

func createWaitForFinishedJobsMeasurement() measurement.Measurement {
	return &waitForFinishedJobsMeasurement{
		selector:     util.NewObjectSelector(),
		queue:        workerqueue.NewWorkerQueue(waitForFinishedJobsWorkers),
		finishedJobs: make(map[string]batchv1.JobConditionType),
	}
}

type waitForFinishedJobsMeasurement struct {
	selector *util.ObjectSelector

	queue            workerqueue.Interface
	isRunning        bool
	clusterFramework *framework.Framework
	cancel           context.CancelFunc

	// lock guards finishedJobs.
	lock         sync.Mutex
	finishedJobs map[string]batchv1.JobConditionType
}

func (w *waitForFinishedJobsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	w.clusterFramework = config.ClusterFramework

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if err = w.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		return nil, w.start()
	case "gather":
		timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForFinishedJobsTimeout)
		if err != nil {
			return nil, err
		}
		return nil, w.gather(timeout)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (w *waitForFinishedJobsMeasurement) Dispose() {
	if !w.isRunning {
		return
	}
	w.isRunning = false
	w.queue.Stop()
	w.cancel()
}

func (w *waitForFinishedJobsMeasurement) String() string {
	return waitForFinishedJobsName
}

// start starts a job informer and queues the updates for evaluation.
func (w *waitForFinishedJobsMeasurement) start() error {
	if w.isRunning {
		klog.V(2).Infof("%v: wait for finished jobs measurement already running", w)
		return nil
	}
	klog.V(2).Infof("%v: starting wait for finished jobs measurement...", w)
	w.isRunning = true
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	c := w.clusterFramework.GetClientSets().GetClient()
	inf := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				w.selector.ApplySelectors(&options)
				return c.BatchV1().Jobs(w.selector.Namespace).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				w.selector.ApplySelectors(&options)
				return c.BatchV1().Jobs(w.selector.Namespace).Watch(ctx, options)
			},
		},
		func(oldObj, newObj interface{}) {
			f := func() {
				w.handleObject(oldObj, newObj)
			}
			w.queue.Add(&f)
		},
	)
	return informer.StartAndSync(inf, ctx.Done(), informerSyncTimeout)
}

// gather waits for all the existing jobs to finish and reports how many
// completed and how many failed.
func (w *waitForFinishedJobsMeasurement) gather(timeout time.Duration) error {
	if !w.isRunning {
		return fmt.Errorf("%v: wait for finished jobs was not started", w)
	}
	klog.V(2).Infof("%v: waiting for finished jobs measurement...", w)
	jobKeys, err := w.jobKeys()
	if err != nil {
		return err
	}

	cond := func() (bool, error) {
		w.lock.Lock()
		defer w.lock.Unlock()
		finishedKeys := make(sets.String, len(w.finishedJobs))
		for k := range w.finishedJobs {
			finishedKeys.Insert(k)
		}
		return jobKeys.Equal(finishedKeys), nil
	}
	if err := wait.Poll(checkFinishedJobsInterval, timeout, cond); err != nil {
		klog.V(2).Infof("Timed out waiting for all jobs to finish: %v", err)
	}
	completed := 0
	failed := 0
	timedOut := sets.NewString()
	w.lock.Lock()
	defer w.lock.Unlock()
	for key := range jobKeys {
		if cond, ok := w.finishedJobs[key]; !ok {
			timedOut.Insert(key)
		} else if cond == batchv1.JobComplete {
			completed++
		} else if cond == batchv1.JobFailed {
			failed++
		}
	}
	if timedOut.Len() != 0 {
		return fmt.Errorf("%d Jobs timed out: %s", timedOut.Len(), strings.Join(timedOut.List(), ", "))
	}
	klog.V(2).Infof("%v: %d/%d Jobs finished, %d completed, %d failed", w, completed+failed, len(jobKeys), completed, failed)
	return nil
}

// handleObject casts the objects into Jobs and records their finished status.
func (w *waitForFinishedJobsMeasurement) handleObject(oldObj, newObj interface{}) {
	var oldJob, newJob *batchv1.Job
	var ok bool
	switch cast := oldObj.(type) {
	case *batchv1.Job:
		oldJob = cast
		ok = true
	case cache.DeletedFinalStateUnknown:
		oldJob, ok = cast.Obj.(*batchv1.Job)
	}
	if oldObj != nil && !ok {
		klog.Errorf("%v: uncastable old object: %v", w, oldObj)
	}
	newJob, ok = newObj.(*batchv1.Job)
	if newObj != nil && !ok {
		klog.Errorf("%v: uncastable new object: %v", w, newObj)
		return
	}
	handleJob := newJob
	if newJob == nil {
		handleJob = oldJob
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(handleJob)
	if err != nil {
		klog.Errorf("Failed obtaining meta key for Job: %v", err)
		return
	}
	completed, condition := finishedJobCondition(newJob)

	w.lock.Lock()
	defer w.lock.Unlock()
	if completed {
		w.finishedJobs[key] = condition
	} else {
		delete(w.finishedJobs, key)
	}
}

// jobKeys returns the keys of all the Jobs in the client the match the selector.
func (w *waitForFinishedJobsMeasurement) jobKeys() (sets.String, error) {
	objs, err := w.clusterFramework.GetClientSets().GetClient().BatchV1().Jobs(w.selector.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: w.selector.LabelSelector,
		FieldSelector: w.selector.FieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	keys := sets.NewString()
	for _, j := range objs.Items {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(&j)
		if err != nil {
			return nil, fmt.Errorf("getting key for Job: %w", err)
		}
		keys.Insert(key)
	}
	return keys, nil
}

// finishedJobCondition returns whether the job finished and with what condition.
func finishedJobCondition(j *batchv1.Job) (bool, batchv1.JobConditionType) {
	if j == nil {
		return false, ""
	}
	for _, cond := range j.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}

		if cond.Type == batchv1.JobComplete || cond.Type == batchv1.JobFailed {
			return true, cond.Type
		}
	}
	return false, ""
}
