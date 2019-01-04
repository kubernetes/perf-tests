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
	"math"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
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
	mutex         sync.Mutex
	createTimes   map[string]metav1.Time
	scheduleTimes map[string]metav1.Time
	runTimes      map[string]metav1.Time
	watchTimes    map[string]metav1.Time
	nodeNames     map[string]string
	threshold     time.Duration
}

// Execute supports two actions:
// - start - Starts to observe pods and pods events.
// - gather - Gathers and prints current pod latency data.
// Does NOT support concurrency. Multiple calls to this measurement
// shouldn't be done within one step.
func (p *podStartupLatencyMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}

	switch action {
	case "start":
		p.namespace, err = util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
		if err != nil {
			return summaries, err
		}
		p.labelSelector, err = util.GetStringOrDefault(config.Params, "labelSelector", "")
		if err != nil {
			return summaries, err
		}
		p.fieldSelector, err = util.GetStringOrDefault(config.Params, "fieldSelector", "")
		if err != nil {
			return summaries, err
		}
		p.threshold, err = util.GetDurationOrDefault(config.Params, "threshold", defaultPodStartupLatencyThreshold)
		if err != nil {
			return summaries, err
		}
		return summaries, p.start(config.ClientSets.GetClient())
	case "gather":
		return p.gather(config.ClientSets.GetClient(), config.Identifier)
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}

}

// Dispose cleans up after the measurement.
func (p *podStartupLatencyMeasurement) Dispose() {
	p.stop()
}

// String returns string representation of this measurement.
func (*podStartupLatencyMeasurement) String() string {
	return podStartupLatencyMeasurementName
}

func (p *podStartupLatencyMeasurement) start(c clientset.Interface) error {
	if p.isRunning {
		glog.Infof("%s: pod startup latancy measurement already running", p)
		return nil
	}
	glog.Infof("%s: starting pod startup latency measurement...", p)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = p.fieldSelector
		options.LabelSelector = p.labelSelector
	}
	listerWatcher := cache.NewFilteredListWatchFromClient(c.CoreV1().RESTClient(), "pods", p.namespace, optionsModifier)
	p.informer = cache.NewSharedInformer(listerWatcher, &corev1.Pod{}, 0)
	p.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			p.checkPod(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.checkPod(newObj)
		},
	})

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
	glog.Infof("%s: gathering pod startup latency measurement...", p)
	if !p.isRunning {
		return []measurement.Summary{}, fmt.Errorf("metric %s has not been started", podStartupLatencyMeasurementName)
	}

	scheduleLag := make([]podLatencyData, 0)
	startupLag := make([]podLatencyData, 0)
	watchLag := make([]podLatencyData, 0)
	schedToWatchLag := make([]podLatencyData, 0)
	e2eLag := make([]podLatencyData, 0)

	p.stop()

	if err := p.gatherScheduleTimes(c); err != nil {
		return []measurement.Summary{}, err
	}
	for key, create := range p.createTimes {
		sched, hasSched := p.scheduleTimes[key]
		if !hasSched {
			glog.Infof("%s: failed to find schedule time for %v", p, key)
		}
		run, ok := p.runTimes[key]
		if !ok {
			glog.Infof("%s: failed to find run time for %v", p, key)
			continue
		}
		watch, ok := p.watchTimes[key]
		if !ok {
			glog.Infof("%s: failed to find watch time for %v", p, key)
			continue
		}
		node, ok := p.nodeNames[key]
		if !ok {
			glog.Infof("%s: failed to find node for %v", p, key)
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

	sort.Sort(podLatencySlice(scheduleLag))
	sort.Sort(podLatencySlice(startupLag))
	sort.Sort(podLatencySlice(watchLag))
	sort.Sort(podLatencySlice(schedToWatchLag))
	sort.Sort(podLatencySlice(e2eLag))

	p.printLatencies(scheduleLag, "worst create-to-schedule latencies")
	p.printLatencies(startupLag, "worst schedule-to-run latencies")
	p.printLatencies(watchLag, "worst run-to-watch latencies")
	p.printLatencies(schedToWatchLag, "worst schedule-to-watch latencies")
	p.printLatencies(e2eLag, "worst e2e latencies")

	podStartupLatency := &podStartupLatency{
		identifier:              identifier,
		CreateToScheduleLatency: extractLatencyMetrics(scheduleLag),
		ScheduleToRunLatency:    extractLatencyMetrics(startupLag),
		RunToWatchLatency:       extractLatencyMetrics(watchLag),
		ScheduleToWatchLatency:  extractLatencyMetrics(schedToWatchLag),
		E2ELatency:              extractLatencyMetrics(e2eLag),
	}

	var err error
	if successRatio := float32(len(e2eLag)) / float32(len(p.createTimes)); successRatio < successfulStartupRatioThreshold {
		err = fmt.Errorf("only %v%% of all pods were scheduled successfully", successRatio*100)
		glog.Errorf("%s: %v", p, err)
	}

	podStartupLatencyThreshold := &measurementutil.LatencyMetric{
		Perc50: p.threshold,
		Perc90: p.threshold,
		Perc99: p.threshold,
	}

	if slosErr := podStartupLatency.E2ELatency.VerifyThreshold(podStartupLatencyThreshold); slosErr != nil {
		err = errors.NewMetricViolationError("pod startup", slosErr.Error())
		glog.Errorf("%s: %v", p, err)
	}
	return []measurement.Summary{podStartupLatency}, err
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

func (p *podStartupLatencyMeasurement) checkPod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	if pod.Status.Phase == corev1.PodRunning {
		p.mutex.Lock()
		defer p.mutex.Unlock()
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
				glog.Errorf("%s: pod %v (%v) is reported to be running, but none of its containers is", p, pod.Name, pod.Namespace)
			}
		}
	}
}

func (p *podStartupLatencyMeasurement) printLatencies(latencies []podLatencyData, header string) {
	metrics := extractLatencyMetrics(latencies)
	glog.Infof("%s: 10%% %s: %v", p, header, latencies[(len(latencies)*9)/10:])
	glog.Infof("%s: perc50: %v, perc90: %v, perc99: %v", p, metrics.Perc50, metrics.Perc90, metrics.Perc99)
}

type podLatencyData struct {
	Name    string
	Node    string
	Latency time.Duration
}

type podLatencySlice []podLatencyData

func (a podLatencySlice) Len() int           { return len(a) }
func (a podLatencySlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a podLatencySlice) Less(i, j int) bool { return a[i].Latency < a[j].Latency }

type podStartupLatency struct {
	identifier              string
	CreateToScheduleLatency measurementutil.LatencyMetric `json:"createToScheduleLatency"`
	ScheduleToRunLatency    measurementutil.LatencyMetric `json:"scheduleToRunLatency"`
	RunToWatchLatency       measurementutil.LatencyMetric `json:"runToWatchLatency"`
	ScheduleToWatchLatency  measurementutil.LatencyMetric `json:"scheduleToWatchLatency"`
	E2ELatency              measurementutil.LatencyMetric `json:"e2eLatency"`
}

// SummaryName returns name of the summary.
func (p *podStartupLatency) SummaryName() string {
	return fmt.Sprintf("%s_%s", podStartupLatencyMeasurementName, p.identifier)
}

// PrintSummary returns summary as a string.
func (p *podStartupLatency) PrintSummary() (string, error) {
	return util.PrettyPrintJSON(podStartupLatencyToPerfData(p))
}

func extractLatencyMetrics(latencies []podLatencyData) measurementutil.LatencyMetric {
	length := len(latencies)
	if length == 0 {
		// Ideally we can return LatencyMetric with some NaN/incorrect values,
		// but 0 is the best we can get for time.Duration type.
		return measurementutil.LatencyMetric{Perc50: 0, Perc90: 0, Perc99: 0}
	}
	perc50 := latencies[int(math.Ceil(float64(length*50)/100))-1].Latency
	perc90 := latencies[int(math.Ceil(float64(length*90)/100))-1].Latency
	perc99 := latencies[int(math.Ceil(float64(length*99)/100))-1].Latency
	return measurementutil.LatencyMetric{Perc50: perc50, Perc90: perc90, Perc99: perc99}
}

func latencyToPerfData(l measurementutil.LatencyMetric, name string) measurementutil.DataItem {
	return measurementutil.DataItem{
		Data: map[string]float64{
			"Perc50": float64(l.Perc50) / 1000000, // ns -> ms
			"Perc90": float64(l.Perc90) / 1000000,
			"Perc99": float64(l.Perc99) / 1000000,
		},
		Unit: "ms",
		Labels: map[string]string{
			"Metric": name,
		},
	}
}

func podStartupLatencyToPerfData(latency *podStartupLatency) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentApiCallMetricsVersion}
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.CreateToScheduleLatency, "create_to_schedule"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.ScheduleToRunLatency, "schedule_to_run"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.RunToWatchLatency, "run_to_watch"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.ScheduleToWatchLatency, "schedule_to_watch"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.E2ELatency, "pod_startup"))
	return perfData
}

func createMetaNamespaceKey(namespace, name string) string {
	return namespace + "/" + name
}
