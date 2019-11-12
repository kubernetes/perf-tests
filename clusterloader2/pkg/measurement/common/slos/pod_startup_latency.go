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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultPodStartupLatencyThreshold = 5 * time.Second
	podStartupLatencyMeasurementName  = "PodStartupLatency"
	informerSyncTimeout               = time.Minute

	createPhase   = "create"
	schedulePhase = "schedule"
	runPhase      = "run"
	watchPhase    = "watch"
)

func init() {
	measurement.Register(podStartupLatencyMeasurementName, createPodStartupLatencyMeasurement)
}

func createPodStartupLatencyMeasurement() measurement.Measurement {
	return &podStartupLatencyMeasurement{
		selector:          measurementutil.NewObjectSelector(),
		podStartupEntries: measurementutil.NewObjectTransitionTimes(podStartupLatencyMeasurementName),
	}
}

type podStartupLatencyMeasurement struct {
	selector          *measurementutil.ObjectSelector
	isRunning         bool
	stopCh            chan struct{}
	podStartupEntries *measurementutil.ObjectTransitionTimes
	threshold         time.Duration
}

// Execute supports two actions:
// - start - Starts to observe pods and pods events.
// - gather - Gathers and prints current pod latency data.
// Does NOT support concurrency. Multiple calls to this measurement
// shouldn't be done within one step.
func (p *podStartupLatencyMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
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
		return nil, p.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		return p.gather(config.ClusterFramework.GetClientSets().GetClient(), config.Identifier)
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
		klog.Infof("%s: pod startup latancy measurement already running", p)
		return nil
	}
	klog.Infof("%s: starting pod startup latency measurement...", p)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	i := informer.NewInformer(
		c,
		"pods",
		p.selector,
		p.checkPod,
	)
	return informer.StartAndSync(i, p.stopCh, informerSyncTimeout)
}

func (p *podStartupLatencyMeasurement) stop() {
	if p.isRunning {
		p.isRunning = false
		close(p.stopCh)
	}
}

func (p *podStartupLatencyMeasurement) gather(c clientset.Interface, identifier string) ([]measurement.Summary, error) {
	klog.Infof("%s: gathering pod startup latency measurement...", p)
	if !p.isRunning {
		return nil, fmt.Errorf("metric %s has not been started", podStartupLatencyMeasurementName)
	}

	p.stop()

	if err := p.gatherScheduleTimes(c); err != nil {
		return nil, err
	}

	podStartupLatency := p.podStartupEntries.CalculateTransitionsLatency(map[string]measurementutil.Transition{
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
			From:      createPhase,
			To:        watchPhase,
			Threshold: p.threshold,
		},
	})

	var err error
	if slosErr := podStartupLatency["pod_startup"].VerifyThreshold(p.threshold); slosErr != nil {
		err = errors.NewMetricViolationError("pod startup", slosErr.Error())
		klog.Errorf("%s: %v", p, err)
	}

	content, jsonErr := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(podStartupLatency))
	if jsonErr != nil {
		return nil, jsonErr
	}
	summary := measurement.CreateSummary(fmt.Sprintf("%s_%s", podStartupLatencyMeasurementName, identifier), "json", content)
	return []measurement.Summary{summary}, err
}

func (p *podStartupLatencyMeasurement) gatherScheduleTimes(c clientset.Interface) error {
	selector := fields.Set{
		"involvedObject.kind": "Pod",
		"source":              corev1.DefaultSchedulerName,
	}.AsSelector().String()
	options := metav1.ListOptions{FieldSelector: selector}
	schedEvents, err := c.CoreV1().Events(p.selector.Namespace).List(options)
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

func (p *podStartupLatencyMeasurement) checkPod(_, obj interface{}) {
	if obj == nil {
		return
	}
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	if pod.Status.Phase == corev1.PodRunning {
		key := createMetaNamespaceKey(pod.Namespace, pod.Name)
		if _, found := p.podStartupEntries.Get(key, createPhase); !found {
			p.podStartupEntries.Set(key, watchPhase, time.Now())
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
