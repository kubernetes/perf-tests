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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

type eventData struct {
	obj      interface{}
	recvTime time.Time
}

const (
	jobLifecycleLatencyMeasurementName = "JobLifecycleLatency"
	checkCompletedJobsInterval         = time.Second
	jobCreated                         = "JobCreated"
	jobStarted                         = "JobStarted"
	jobCompleted                       = "JobCompleted"
)

func init() {
	if err := measurement.Register(jobLifecycleLatencyMeasurementName, createJobLifecycleLatencyMeasurement); err != nil {
		klog.Fatalf("Can't register service %v", err)
	}
}

func createJobLifecycleLatencyMeasurement() measurement.Measurement {
	return &jobLifecycleLatencyMeasurement{
		selector:        util.NewObjectSelector(),
		jobStateEntries: measurementutil.NewObjectTransitionTimes(jobLifecycleLatencyMeasurementName),
		eventQueue:      workqueue.NewTyped[*eventData](),
	}
}

type jobLifecycleLatencyMeasurement struct {
	selector        *util.ObjectSelector
	isRunning       bool
	stopCh          chan struct{}
	eventQueue      workqueue.TypedInterface[*eventData]
	jobStateEntries *measurementutil.ObjectTransitionTimes
}

// Execute supports two actions:
// - start - Starts to observe jobs and their state transitions.
// - gather - Gathers and prints job latency data.
// heavily influenced by pod_startup_latency measurement
func (p *jobLifecycleLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		if err := p.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		return nil, p.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForFinishedJobsTimeout)
		if err != nil {
			return nil, err
		}
		return p.gather(config.Identifier, timeout)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}

}

// Dispose cleans up after the measurement.
func (p *jobLifecycleLatencyMeasurement) Dispose() {
	p.stop()
}

// String returns string representation of this measurement.
func (p *jobLifecycleLatencyMeasurement) String() string {
	return jobLifecycleLatencyMeasurementName + ": " + p.selector.String()
}

func (p *jobLifecycleLatencyMeasurement) start(c clientset.Interface) error {
	if p.isRunning {
		klog.V(2).Infof("%s: job lifecycle latency measurement already running", p)
		return nil
	}
	klog.V(2).Infof("%s: starting job lifecycle latency measurement...", p)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	i := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				p.selector.ApplySelectors(&options)
				return c.BatchV1().Jobs(p.selector.Namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				p.selector.ApplySelectors(&options)
				return c.BatchV1().Jobs(p.selector.Namespace).Watch(context.TODO(), options)
			},
		},
		p.addEvent,
	)
	go p.processEvents()
	return informer.StartAndSync(i, p.stopCh, informerSyncTimeout)
}

func (p *jobLifecycleLatencyMeasurement) addEvent(_, obj interface{}) {
	event := &eventData{obj: obj, recvTime: time.Now()}
	p.eventQueue.Add(event)
}

func (p *jobLifecycleLatencyMeasurement) processEvents() {
	for p.processNextWorkItem() {
	}
}

func (p *jobLifecycleLatencyMeasurement) processNextWorkItem() bool {
	item, quit := p.eventQueue.Get()
	if quit {
		return false
	}
	defer p.eventQueue.Done(item)
	p.processEvent(item)
	return true
}

// processEvent processes job state change events:
// uses Phase Latency utility to record job state transitions
// it currently captures the following transitions:
// JobCreated (job.CreationTimestamp.Time) -> JobStarted (job.Status.StartTime.Time)
// JobStarted (job.Status.StartTime.Time) -> JobCompleted (job.Status.CompletionTime.Time)
func (p *jobLifecycleLatencyMeasurement) processEvent(event *eventData) {
	obj := event.obj
	if obj == nil {
		return
	}
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return
	}
	key := createMetaNamespaceKey(job.Namespace, job.Name)
	if _, found := p.jobStateEntries.Get(key, jobCreated); !found {
		p.jobStateEntries.Set(key, jobCreated, job.CreationTimestamp.Time)
	}
	if job.Status.StartTime != nil {
		if _, found := p.jobStateEntries.Get(key, jobStarted); !found {
			p.jobStateEntries.Set(key, jobStarted, job.Status.StartTime.Time)
		}
	}
	if job.Status.CompletionTime != nil {
		if _, found := p.jobStateEntries.Get(key, jobCompleted); !found {
			p.jobStateEntries.Set(key, jobCompleted, job.Status.CompletionTime.Time)
		}
	}
}

func (p *jobLifecycleLatencyMeasurement) stop() {
	if p.isRunning {
		p.isRunning = false
		close(p.stopCh)
		p.eventQueue.ShutDown()
	}
}

var jobLifecycleTransitions = map[string]measurementutil.Transition{
	"create_to_start": {
		From: jobCreated,
		To:   jobStarted,
	},
	"start_to_complete": {
		From: jobStarted,
		To:   jobCompleted,
	},
}

// gather collects job lifecycle latency and calculates percentiles using Phase Latency utility
// it waits for all jobs to be completed before collecting the metrics or times out
func (p *jobLifecycleLatencyMeasurement) gather(identifier string, timeout time.Duration) ([]measurement.Summary, error) {
	klog.V(2).Infof("%s: gathering job lifecycle latency measurement...", p)
	if !p.isRunning {
		return nil, fmt.Errorf("metric %s has not been started", jobLifecycleLatencyMeasurementName)
	}
	condition := func() (bool, error) {
		return p.jobStateEntries.Count(jobCreated) == p.jobStateEntries.Count(jobCompleted), nil
	}
	if err := wait.Poll(checkCompletedJobsInterval, timeout, condition); err != nil {
		klog.V(2).Infof("Timed out waiting for all jobs to complete: %v", err)
	}
	p.stop()
	jobLifecycleLatency := p.jobStateEntries.CalculateTransitionsLatency(jobLifecycleTransitions, measurementutil.MatchAll)
	content, jsonErr := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(jobLifecycleLatency))
	if jsonErr != nil {
		return nil, jsonErr
	}
	summaryName := fmt.Sprintf("%s_%s", jobLifecycleLatencyMeasurementName, identifier)
	summaries := []measurement.Summary{measurement.CreateSummary(summaryName, "json", content)}
	return summaries, nil
}

func createMetaNamespaceKey(namespace, name string) string {
	return namespace + "/" + name
}
