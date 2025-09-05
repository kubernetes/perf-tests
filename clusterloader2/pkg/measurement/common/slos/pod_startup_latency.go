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

package slos

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultPodStartupLatencyThreshold = 5 * time.Second
	defaultSchedulerName              = corev1.DefaultSchedulerName
	podStartupLatencyMeasurementName  = "PodStartupLatency"
	informerSyncTimeout               = time.Minute

	createPhase   = "create"
	schedulePhase = "schedule"
	runPhase      = "run"
	watchPhase    = "watch"
)

func init() {
	if err := measurement.Register(podStartupLatencyMeasurementName, createPodStartupLatencyMeasurement); err != nil {
		klog.Fatalf("cant register service %v", err)
	}
}

func createPodStartupLatencyMeasurement() measurement.Measurement {
	return &podStartupLatencyMeasurement{
		selector:          util.NewObjectSelector(),
		podStartupEntries: measurementutil.NewObjectTransitionTimes(podStartupLatencyMeasurementName),
		podMetadata:       measurementutil.NewPodsMetadata(podStartupLatencyMeasurementName),
		eventQueue:        workqueue.NewTyped[*eventData](),
	}
}

type eventData struct {
	obj      interface{}
	recvTime time.Time
}

type podStartupLatencyMeasurement struct {
	selector  *util.ObjectSelector
	isRunning bool
	stopCh    chan struct{}
	// This queue can potentially grow indefinitely, beacause we put all changes here.
	// Usually it's not recommended pattern, but we need it for measuring PodStartupLatency.
	eventQueue        workqueue.TypedInterface[*eventData]
	podStartupEntries *measurementutil.ObjectTransitionTimes
	podMetadata       *measurementutil.PodsMetadata
	threshold         time.Duration
	// Threshold for pod startup latency by percentile. The default value is threshold.
	perc50Threshold time.Duration
	perc90Threshold time.Duration
	perc99Threshold time.Duration
}

// Execute supports two actions:
// - start - Starts to observe pods and pods events.
// - gather - Gathers and prints current pod latency data.
// Does NOT support concurrency. Multiple calls to this measurement
// shouldn't be done within one step.
func (p *podStartupLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if err := p.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		p.threshold, err = util.GetDurationOrDefault(config.Params, "threshold", defaultPodStartupLatencyThreshold)
		if err != nil {
			return nil, err
		}
		p.perc50Threshold, err = util.GetDurationOrDefault(config.Params, "perc50Threshold", p.threshold)
		if err != nil {
			return nil, err
		}
		p.perc90Threshold, err = util.GetDurationOrDefault(config.Params, "perc90Threshold", p.threshold)
		if err != nil {
			return nil, err
		}
		p.perc99Threshold, err = util.GetDurationOrDefault(config.Params, "perc99Threshold", p.threshold)
		if err != nil {
			return nil, err
		}
		return nil, p.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		schedulerName, err := util.GetStringOrDefault(config.Params, "schedulerName", defaultSchedulerName)
		if err != nil {
			return nil, err
		}
		return p.gather(config.ClusterFramework.GetClientSets().GetClient(), config.Identifier, schedulerName)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}

}

// Dispose cleans up after the measurement.
func (p *podStartupLatencyMeasurement) Dispose() {
	p.stop()
}

// String returns string representation of this measurement.
func (p *podStartupLatencyMeasurement) String() string {
	return podStartupLatencyMeasurementName + ": " + p.selector.String()
}

func (p *podStartupLatencyMeasurement) start(c clientset.Interface) error {
	if p.isRunning {
		klog.V(2).Infof("%s: pod startup latancy measurement already running", p)
		return nil
	}
	klog.V(2).Infof("%s: starting pod startup latency measurement...", p)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	i := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				p.selector.ApplySelectors(&options)
				return c.CoreV1().Pods(p.selector.Namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				p.selector.ApplySelectors(&options)
				return c.CoreV1().Pods(p.selector.Namespace).Watch(context.TODO(), options)
			},
		},
		p.addEvent,
	)
	go p.processEvents()
	return informer.StartAndSync(i, p.stopCh, informerSyncTimeout)
}

func (p *podStartupLatencyMeasurement) addEvent(_, obj interface{}) {
	event := &eventData{obj: obj, recvTime: time.Now()}
	p.eventQueue.Add(event)
}

func (p *podStartupLatencyMeasurement) processEvents() {
	for p.processNextWorkItem() {
	}
}

func (p *podStartupLatencyMeasurement) processNextWorkItem() bool {
	item, quit := p.eventQueue.Get()
	if quit {
		return false
	}
	defer p.eventQueue.Done(item)

	p.processEvent(item)
	return true
}

func (p *podStartupLatencyMeasurement) stop() {
	if p.isRunning {
		p.isRunning = false
		close(p.stopCh)
		p.eventQueue.ShutDown()
	}
}

var podStartupTransitions = map[string]measurementutil.Transition{
	"create_to_schedule": {
		From: createPhase,
		To:   schedulePhase,
	},
	"schedule_to_run": {
		From: schedulePhase,
		To:   runPhase,
	},
	"run_to_watch": {
		From: runPhase,
		To:   watchPhase,
	},
	"schedule_to_watch": {
		From: schedulePhase,
		To:   watchPhase,
	},
	"pod_startup": {
		From: createPhase,
		To:   watchPhase,
	},
}

func podStartupTransitionsWithThreshold(threshold time.Duration) map[string]measurementutil.Transition {
	result := make(map[string]measurementutil.Transition)
	for key, value := range podStartupTransitions {
		result[key] = value
	}
	podStartupTransition := result["pod_startup"]
	podStartupTransition.Threshold = threshold
	result["pod_startup"] = podStartupTransition
	return result
}

type podStartupLatencyCheck struct {
	namePrefix string
	filter     measurementutil.KeyFilterFunc
}

func (p *podStartupLatencyMeasurement) gather(c clientset.Interface, identifier string, schedulerName string) ([]measurement.Summary, error) {
	klog.V(2).Infof("%s: gathering pod startup latency measurement...", p)
	if !p.isRunning {
		return nil, fmt.Errorf("metric %s has not been started", podStartupLatencyMeasurementName)
	}

	p.stop()

	if err := p.gatherScheduleTimes(c, schedulerName); err != nil {
		return nil, err
	}

	checks := []podStartupLatencyCheck{
		{
			namePrefix: "",
			filter:     measurementutil.MatchAll,
		},
		{
			namePrefix: "Stateless",
			filter:     p.podMetadata.FilterStateless,
		},
		{
			namePrefix: "Stateful",
			filter:     p.podMetadata.FilterStateful,
		},
	}

	var summaries []measurement.Summary
	var err error
	for _, check := range checks {
		transitions := podStartupTransitionsWithThreshold(p.threshold)
		podStartupLatency := p.podStartupEntries.CalculateTransitionsLatency(transitions, check.filter)

		if slosErr := podStartupLatency["pod_startup"].VerifyThresholdByPercentile(p.perc50Threshold, p.perc90Threshold, p.perc99Threshold); slosErr != nil {
			err = errors.NewMetricViolationError("pod startup", slosErr.Error())
			klog.Errorf("%s%s: %v", check.namePrefix, p, err)
		}

		content, jsonErr := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(podStartupLatency))
		if jsonErr != nil {
			return nil, jsonErr
		}
		summaryName := fmt.Sprintf("%s%s_%s", check.namePrefix, podStartupLatencyMeasurementName, identifier)
		summaries = append(summaries, measurement.CreateSummary(summaryName, "json", content))
	}
	return summaries, err
}

// TODO(#2006): gatherScheduleTimes is currently listing events at the end of the test.
//
//	Given that events by default have 1h TTL, for measurements across longer periods
//	it just returns incomplete results.
//	Given that we don't 100% accuracy, we should switch to a mechanism that is similar
//	to the one that slo-monitor is using (added in #1477).
func (p *podStartupLatencyMeasurement) gatherScheduleTimes(c clientset.Interface, schedulerName string) error {
	selector := fields.Set{
		"involvedObject.kind": "Pod",
		"source":              schedulerName,
	}.AsSelector().String()
	options := metav1.ListOptions{FieldSelector: selector}
	schedEvents, err := c.CoreV1().Events(p.selector.Namespace).List(context.TODO(), options)
	if err != nil {
		return err
	}
	for _, event := range schedEvents.Items {
		key := createMetaNamespaceKey(event.InvolvedObject.Namespace, event.InvolvedObject.Name)
		if _, exists := p.podStartupEntries.Get(key, createPhase); exists {
			if !event.EventTime.IsZero() {
				p.podStartupEntries.Set(key, schedulePhase, event.EventTime.Time)
			} else {
				p.podStartupEntries.Set(key, schedulePhase, event.FirstTimestamp.Time)
			}
		}
	}
	return nil
}

func (p *podStartupLatencyMeasurement) processEvent(event *eventData) {
	obj, recvTime := event.obj, event.recvTime
	if obj == nil {
		return
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	key := createMetaNamespaceKey(pod.Namespace, pod.Name)
	p.podMetadata.SetStateless(key, isPodStateless(pod))

	if pod.Status.Phase == corev1.PodRunning {
		if _, found := p.podStartupEntries.Get(key, createPhase); !found {
			p.podStartupEntries.Set(key, watchPhase, recvTime)
			p.podStartupEntries.Set(key, createPhase, pod.CreationTimestamp.Time)
			var startTime metav1.Time
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Running != nil {
					if startTime.Before(&cs.State.Running.StartedAt) {
						startTime = cs.State.Running.StartedAt
					}
				}
			}
			if startTime != metav1.NewTime(time.Time{}) {
				p.podStartupEntries.Set(key, runPhase, startTime.Time)
			} else {
				klog.Errorf("%s: pod %v (%v) is reported to be running, but none of its containers is", p, pod.Name, pod.Namespace)
			}
		}
	}
}

func createMetaNamespaceKey(namespace, name string) string {
	return namespace + "/" + name
}

func isPodStateless(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir != nil || volume.DownwardAPI != nil || volume.ConfigMap != nil || volume.Secret != nil || volume.Projected != nil {
			continue
		}
		klog.V(4).Infof("pod %s/%s classified as stateful", pod.Namespace, pod.Name)
		return false
	}
	return true
}
