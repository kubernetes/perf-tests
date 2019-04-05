/*
Copyright 2017 The Kubernetes Authors.

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

package monitors

import (
	"context"
	"strings"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/pager"

	kubeletevents "k8s.io/kubernetes/pkg/kubelet/events"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// PodE2EStartupLatencyKey is a key for pod startup latency monitoring metric.
	PodE2EStartupLatencyKey = "slomonitor_pod_e2e_startup_latency_seconds"
	// PodFullStartupLatencyKey is a key for pod startup latency monitoring metric including pull times.
	PodFullStartupLatencyKey = "slomonitor_pod_full_startup_latency_seconds"
)

var (
	// StartupLatencyBuckets represents the histogram bucket boundaries for pod
	// startup latency metrics, measured in seconds. These are hand-picked so
	// as to be roughly exponential but still round numbers in everyday units.
	// This is to minimise the number of buckets while allowing accurate
	// measurement of thresholds which might be used in SLOs e.g. x% of pods
	// start up within 30 seconds, or 15 minutes, etc.
	StartupLatencyBuckets = []float64{0.5, 1, 2, 3, 4, 5, 6, 8, 10, 20, 30, 45, 60, 120, 180, 240, 300, 360, 480, 600, 900, 1200, 1800, 2700, 3600}

	// PodE2EStartupLatency is a prometheus metric for monitoring pod startup latency.
	PodE2EStartupLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    PodE2EStartupLatencyKey,
			Help:    "Pod e2e startup latencies in seconds, without image pull times",
			Buckets: StartupLatencyBuckets,
		},
	)

	// PodFullStartupLatency is a prometheus metric for monitoring pod startup latency including image pull times.
	PodFullStartupLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    PodFullStartupLatencyKey,
			Help:    "Pod e2e startup latencies in seconds, with image pull times",
			Buckets: StartupLatencyBuckets,
		},
	)
)

var registerMetrics sync.Once

// Register registers all prometheus metrics.
func Register() {
	registerMetrics.Do(func() {
		prometheus.MustRegister(PodE2EStartupLatency)
		prometheus.MustRegister(PodFullStartupLatency)
	})
}

// PodStartupMilestones keeps all milestone timestamps from Pod creation.
type PodStartupMilestones struct {
	created          time.Time
	startedPulling   time.Time
	finishedPulling  time.Time
	observedRunning  time.Time
	deletedTimestamp time.Time
	seenPulled       int
	needPulled       int
	// Flag that says if given data was already used to add a datapoint to the metric.
	accountedFor bool
}

// PodStartupLatencyDataMonitor monitors pod startup latency and exposes prometheus metric.
type PodStartupLatencyDataMonitor struct {
	sync.Mutex
	kubeClient     clientset.Interface
	PodStartupData map[string]PodStartupMilestones
	// Slice of strings marked for deletion waiting to be purged. Just an optimization to avoid
	// full scans.
	toDelete []string
	// Time after which deleted entries are purged. We don't want to delete data right after observing deletions,
	// because we may get events corresponding to the given Pod later, which would result in recreating entry,
	// and a serious memory leak.
	purgeAfter time.Duration
}

// IsComplete returns true is data is complete (ready to be included in the metric) and if it haven't been included in the metric yet.
func (data *PodStartupMilestones) IsComplete() bool {
	return !data.created.IsZero() && !data.startedPulling.IsZero() && !data.finishedPulling.IsZero() && !data.observedRunning.IsZero() && data.seenPulled == data.needPulled && !data.accountedFor
}

// NewPodStartupLatencyDataMonitor creates a new PodStartupLatencyDataMonitor from a given client.
func NewPodStartupLatencyDataMonitor(c clientset.Interface, purgeAfter time.Duration) *PodStartupLatencyDataMonitor {
	return &PodStartupLatencyDataMonitor{
		kubeClient:     c,
		PodStartupData: map[string]PodStartupMilestones{},
		purgeAfter:     purgeAfter,
	}
}

// Run starts a PodStartupLatencyDataMonitor: it creates all watches, populates PodStartupData and updates
// PodE2EStartupLatency and PodFullStartupLatency metrics
func (pm *PodStartupLatencyDataMonitor) Run(stopCh chan struct{}) error {
	controller := NewWatcherWithHandler(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				pg := pager.New(pager.SimplePageFunc(func(opts metav1.ListOptions) (runtime.Object, error) {
					return pm.kubeClient.CoreV1().Pods(v1.NamespaceAll).List(opts)
				}))
				return pg.List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return pm.kubeClient.CoreV1().Pods(v1.NamespaceAll).Watch(options)
			},
		},
		&v1.Pod{},
		func(obj interface{}) error {
			p, ok := obj.(*v1.Pod)
			if !ok {
				glog.Errorf("Failed to cast observed object to *v1.Pod: %v", obj)
				return nil
			}
			pm.handlePodUpdate(p)
			return nil
		},
		func(obj interface{}) error {
			p, ok := obj.(*v1.Pod)
			if !ok {
				if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					p, ok = d.Obj.(*v1.Pod)
					if !ok {
						glog.Errorf("Failed to cast embedded object from tombstone to *v1.Pod: %v", d.Obj)
						return nil
					}
				} else {
					glog.Errorf("Failed to cast observed object to *v1.Pod: %v", obj)
					return nil
				}
			}
			pm.handlePodDelete(p)
			return nil
		},
	)
	go controller.Run(stopCh)

	eventSelector := fields.Set{
		"involvedObject.kind": "Pod",
		// this is actually source.Component
		"source": "kubelet",
	}.AsSelector().String()

	eventController := NewWatcherWithHandler(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = eventSelector
				pg := pager.New(pager.SimplePageFunc(func(opts metav1.ListOptions) (runtime.Object, error) {
					return pm.kubeClient.CoreV1().Events(v1.NamespaceAll).List(opts)
				}))
				return pg.List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = eventSelector
				return pm.kubeClient.CoreV1().Events(v1.NamespaceAll).Watch(options)
			},
		},
		&v1.Event{},
		func(obj interface{}) error {
			e, ok := obj.(*v1.Event)
			if !ok {
				glog.Errorf("Failed to cast observed object to *v1.Event: %v", obj)
				return nil
			}
			pm.handleEventUpdate(e)
			return nil
		},
		func(obj interface{}) error {
			return nil
		},
	)
	go eventController.Run(stopCh)
	wait.Until(pm.purgeOutdated, 15*time.Second, stopCh)
	return nil
}

func (pm *PodStartupLatencyDataMonitor) purgeOutdated() {
	now := time.Now()
	pm.Lock()
	defer pm.Unlock()
	var left []string
	for _, podKey := range pm.toDelete {
		if data, ok := pm.PodStartupData[podKey]; ok {
			if data.deletedTimestamp.Add(pm.purgeAfter).After(now) {
				left = append(left, podKey)
				continue
			}
			delete(pm.PodStartupData, podKey)
		}
	}
	pm.toDelete = left
}

func (pm *PodStartupLatencyDataMonitor) handlePodUpdate(p *v1.Pod) {
	if isReady, create, running := checkPodAndGetStartupLatency(p); isReady {
		go pm.insertPodRunningTime(getPodKey(p), create, running, len(p.Spec.Containers))
	}
}

func (pm *PodStartupLatencyDataMonitor) handlePodDelete(p *v1.Pod) {
	pm.Lock()
	defer pm.Unlock()

	key := getPodKey(p)
	ok := false
	data := PodStartupMilestones{}
	if data, ok = pm.PodStartupData[key]; !ok {
		data = PodStartupMilestones{}
		data.startedPulling = time.Unix(0, 0)
	}
	data.deletedTimestamp = time.Now()
	pm.PodStartupData[key] = data
	pm.toDelete = append(pm.toDelete, key)
}

func (pm *PodStartupLatencyDataMonitor) handlePullingImageEvent(key string, e *v1.Event) {
	pm.Lock()
	defer pm.Unlock()

	ok := false
	data := PodStartupMilestones{}
	if data, ok = pm.PodStartupData[key]; !ok {
		data.finishedPulling = time.Unix(0, 0)
		data.needPulled = -1
	}
	if data.startedPulling.IsZero() || data.startedPulling.After(e.FirstTimestamp.Time) {
		data.startedPulling = e.FirstTimestamp.Time
	}

	pm.updateMetric(key, &data)
	pm.PodStartupData[key] = data
}

func (pm *PodStartupLatencyDataMonitor) handlePulledImageEvent(key string, e *v1.Event) {
	pm.Lock()
	defer pm.Unlock()

	ok := false
	data := PodStartupMilestones{}
	if data, ok = pm.PodStartupData[key]; ok {
		data.startedPulling = time.Unix(0, 0)
		data.needPulled = -1
	}
	// We need to distinguish between "real" pulled and one that's a result of image already present.
	if data.finishedPulling.IsZero() || data.finishedPulling.Before(e.FirstTimestamp.Time) {
		data.finishedPulling = e.FirstTimestamp.Time
	}
	if strings.Contains(e.Message, "already present on machine") {
		data.startedPulling = e.FirstTimestamp.Time
	}
	data.seenPulled++

	pm.updateMetric(key, &data)
	pm.PodStartupData[key] = data
}

func (pm *PodStartupLatencyDataMonitor) handleEventUpdate(e *v1.Event) {
	key := getPodKeyFromReference(&e.InvolvedObject)
	switch e.Reason {
	case kubeletevents.PullingImage:
		go pm.handlePullingImageEvent(key, e)
	case kubeletevents.PulledImage:
		go pm.handlePulledImageEvent(key, e)
	default:
		return
	}
}

func (pm *PodStartupLatencyDataMonitor) updateMetric(key string, data *PodStartupMilestones) {
	if data.IsComplete() {
		glog.V(4).Infof("Observed Pod %v creation: created %v, pulling: %v, pulled: %v, running: %v",
			key, data.created, data.startedPulling, data.finishedPulling, data.observedRunning)
		data.accountedFor = true
		fullStartupTime := data.observedRunning.Sub(data.created)
		startupTime := fullStartupTime - data.finishedPulling.Sub(data.startedPulling)
		if startupTime < 0 {
			glog.Warningf("Saw negative startup time for %v: %v", key, data)
			startupTime = 0
		}
		PodE2EStartupLatency.Observe(startupTime.Seconds())
		PodFullStartupLatency.Observe(fullStartupTime.Seconds())
	}
}

func (pm *PodStartupLatencyDataMonitor) insertPodRunningTime(podKey string, createTime time.Time, runningTime time.Time, needPulled int) {
	pm.Lock()
	defer pm.Unlock()

	var data PodStartupMilestones
	var ok bool
	if data, ok = pm.PodStartupData[podKey]; !ok {
		// Necessary to work anywhere except UTC time zone...
		data.startedPulling = time.Unix(0, 0)
		data.finishedPulling = time.Unix(0, 0)
	}
	data.created = createTime
	data.observedRunning = runningTime
	data.needPulled = needPulled

	pm.updateMetric(podKey, &data)
	pm.PodStartupData[podKey] = data
}
