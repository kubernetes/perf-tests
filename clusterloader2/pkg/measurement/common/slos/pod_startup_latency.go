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
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
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
	successfulStartupRatioThreshold   = 0.99
)

func init() {
	measurement.Register(podStartupLatencyMeasurementName, createPodStartupLatencyMeasurement)
}

func createPodStartupLatencyMeasurement() measurement.Measurement {
	return &podStartupLatencyMeasurement{
		createTimes:   make(map[string]metav1.Time),
		scheduleTimes: make(map[string]metav1.Time),
		runTimes:      make(map[string]metav1.Time),
		watchTimes:    make(map[string]metav1.Time),
		nodeNames:     make(map[string]string),
	}
}

type podStartupLatencyMeasurement struct {
	namespace     string
	labelSelector string
	fieldSelector string
	informer      cache.SharedInformer
	isRunning     bool
	stopCh        chan struct{}

	lock            sync.Mutex
	createTimes     map[string]metav1.Time
	scheduleTimes   map[string]metav1.Time
	runTimes        map[string]metav1.Time
	watchTimes      map[string]metav1.Time
	nodeNames       map[string]string
	threshold       time.Duration
	selectorsString string
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
		p.namespace, err = util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
		if err != nil {
			return nil, err
		}
		p.labelSelector, err = util.GetStringOrDefault(config.Params, "labelSelector", "")
		if err != nil {
			return nil, err
		}
		p.fieldSelector, err = util.GetStringOrDefault(config.Params, "fieldSelector", "")
		if err != nil {
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
	return podStartupLatencyMeasurementName + ": " + p.selectorsString
}

func (p *podStartupLatencyMeasurement) start(c clientset.Interface) error {
	if p.isRunning {
		klog.Infof("%s: pod startup latancy measurement already running", p)
		return nil
	}
	p.selectorsString = measurementutil.CreateSelectorsString(p.namespace, p.labelSelector, p.fieldSelector)
	klog.Infof("%s: starting pod startup latency measurement...", p)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	p.informer = informer.NewInformer(
		c,
		"pods",
		p.namespace,
		p.fieldSelector,
		p.labelSelector,
		p.checkPod,
	)

	go p.informer.Run(p.stopCh)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerSyncTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, p.informer.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}
	return nil
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

	scheduleLag := make([]measurementutil.LatencyData, 0)
	startupLag := make([]measurementutil.LatencyData, 0)
	watchLag := make([]measurementutil.LatencyData, 0)
	schedToWatchLag := make([]measurementutil.LatencyData, 0)
	e2eLag := make([]measurementutil.LatencyData, 0)

	p.stop()

	if err := p.gatherScheduleTimes(c); err != nil {
		return nil, err
	}
	for key, create := range p.createTimes {
		sched, hasSched := p.scheduleTimes[key]
		if !hasSched {
			klog.Infof("%s: failed to find schedule time for %v", p, key)
		}
		run, ok := p.runTimes[key]
		if !ok {
			klog.Infof("%s: failed to find run time for %v", p, key)
			continue
		}
		watch, ok := p.watchTimes[key]
		if !ok {
			klog.Infof("%s: failed to find watch time for %v", p, key)
			continue
		}
		node, ok := p.nodeNames[key]
		if !ok {
			klog.Infof("%s: failed to find node for %v", p, key)
			continue
		}

		if hasSched {
			scheduleLag = append(scheduleLag, podLatencyData{Name: key, Node: node, Latency: sched.Time.Sub(create.Time)})
			startupLag = append(startupLag, podLatencyData{Name: key, Node: node, Latency: run.Time.Sub(sched.Time)})
			schedToWatchLag = append(schedToWatchLag, podLatencyData{Name: key, Node: node, Latency: watch.Time.Sub(sched.Time)})
		}
		watchLag = append(watchLag, podLatencyData{Name: key, Node: node, Latency: watch.Time.Sub(run.Time)})
		e2eLag = append(e2eLag, podLatencyData{Name: key, Node: node, Latency: watch.Time.Sub(create.Time)})
	}

	sort.Sort(measurementutil.LatencySlice(scheduleLag))
	sort.Sort(measurementutil.LatencySlice(startupLag))
	sort.Sort(measurementutil.LatencySlice(watchLag))
	sort.Sort(measurementutil.LatencySlice(schedToWatchLag))
	sort.Sort(measurementutil.LatencySlice(e2eLag))

	p.printLatencies(scheduleLag, "worst create-to-schedule latencies")
	p.printLatencies(startupLag, "worst schedule-to-run latencies")
	p.printLatencies(watchLag, "worst run-to-watch latencies")
	p.printLatencies(schedToWatchLag, "worst schedule-to-watch latencies")
	p.printLatencies(e2eLag, "worst e2e latencies")

	podStartupLatency := &podStartupLatency{
		CreateToScheduleLatency: measurementutil.ExtractLatencyMetrics(scheduleLag),
		ScheduleToRunLatency:    measurementutil.ExtractLatencyMetrics(startupLag),
		RunToWatchLatency:       measurementutil.ExtractLatencyMetrics(watchLag),
		ScheduleToWatchLatency:  measurementutil.ExtractLatencyMetrics(schedToWatchLag),
		E2ELatency:              measurementutil.ExtractLatencyMetrics(e2eLag),
	}

	var err error
	if successRatio := float32(len(e2eLag)) / float32(len(p.createTimes)); successRatio < successfulStartupRatioThreshold {
		err = fmt.Errorf("only %v%% of all pods were scheduled successfully", successRatio*100)
		klog.Errorf("%s: %v", p, err)
	}

	podStartupLatencyThreshold := &measurementutil.LatencyMetric{
		Perc50: p.threshold,
		Perc90: p.threshold,
		Perc99: p.threshold,
	}

	if slosErr := podStartupLatency.E2ELatency.VerifyThreshold(podStartupLatencyThreshold); slosErr != nil {
		err = errors.NewMetricViolationError("pod startup", slosErr.Error())
		klog.Errorf("%s: %v", p, err)
	}

	content, jsonErr := util.PrettyPrintJSON(podStartupLatencyToPerfData(podStartupLatency))
	if err != nil {
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
	schedEvents, err := c.CoreV1().Events(p.namespace).List(options)
	if err != nil {
		return err
	}
	for _, event := range schedEvents.Items {
		key := createMetaNamespaceKey(event.InvolvedObject.Namespace, event.InvolvedObject.Name)
		if _, ok := p.createTimes[key]; ok {
			p.scheduleTimes[key] = event.FirstTimestamp
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
		p.lock.Lock()
		defer p.lock.Unlock()
		key := createMetaNamespaceKey(pod.Namespace, pod.Name)
		if _, found := p.watchTimes[key]; !found {
			p.watchTimes[key] = metav1.Now()
			p.createTimes[key] = pod.CreationTimestamp
			p.nodeNames[key] = pod.Spec.NodeName
			var startTime metav1.Time
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Running != nil {
					if startTime.Before(&cs.State.Running.StartedAt) {
						startTime = cs.State.Running.StartedAt
					}
				}
			}
			if startTime != metav1.NewTime(time.Time{}) {
				p.runTimes[key] = startTime
			} else {
				klog.Errorf("%s: pod %v (%v) is reported to be running, but none of its containers is", p, pod.Name, pod.Namespace)
			}
		}
	}
}

func (p *podStartupLatencyMeasurement) printLatencies(latencies []measurementutil.LatencyData, header string) {
	metrics := measurementutil.ExtractLatencyMetrics(latencies)
	index := len(latencies) - 100
	if index < 0 {
		index = 0
	}
	klog.Infof("%s: %d %s: %v", p, len(latencies)-index, header, latencies[index:])
	klog.Infof("%s: perc50: %v, perc90: %v, perc99: %v; threshold: %v", p, metrics.Perc50, metrics.Perc90, metrics.Perc99, p.threshold)
}

type podLatencyData struct {
	Name    string
	Node    string
	Latency time.Duration
}

func (p podLatencyData) GetLatency() time.Duration {
	return p.Latency
}

type podStartupLatency struct {
	CreateToScheduleLatency measurementutil.LatencyMetric `json:"createToScheduleLatency"`
	ScheduleToRunLatency    measurementutil.LatencyMetric `json:"scheduleToRunLatency"`
	RunToWatchLatency       measurementutil.LatencyMetric `json:"runToWatchLatency"`
	ScheduleToWatchLatency  measurementutil.LatencyMetric `json:"scheduleToWatchLatency"`
	E2ELatency              measurementutil.LatencyMetric `json:"e2eLatency"`
}

func podStartupLatencyToPerfData(latency *podStartupLatency) *measurementutil.PerfData {
	const nsToMs = 1000000.0
	perfData := &measurementutil.PerfData{Version: currentAPICallMetricsVersion}
	perfData.DataItems = append(perfData.DataItems, latency.CreateToScheduleLatency.ToPerfData("create_to_schedule", nsToMs))
	perfData.DataItems = append(perfData.DataItems, latency.ScheduleToRunLatency.ToPerfData("schedule_to_run", nsToMs))
	perfData.DataItems = append(perfData.DataItems, latency.RunToWatchLatency.ToPerfData("run_to_watch", nsToMs))
	perfData.DataItems = append(perfData.DataItems, latency.ScheduleToWatchLatency.ToPerfData("schedule_to_watch", nsToMs))
	perfData.DataItems = append(perfData.DataItems, latency.E2ELatency.ToPerfData("pod_startup", nsToMs))
	return perfData
}

func createMetaNamespaceKey(namespace, name string) string {
	return namespace + "/" + name
}
