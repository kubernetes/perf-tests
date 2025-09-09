/*
Copyright 2025 The Kubernetes Authors.

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
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1beta2 "k8s.io/api/resource/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// resourceClaimAllocationLatencyMeasurementName is the name of measurement which starts
// a watch on resource claims and records the time it takes for the claim's status to
// be non-empty.
const resourceClaimAllocationLatencyMeasurementName = "ResourceClaimAllocationLatency"

var defaultClaimAllocationLatencyThreshold = 5 * time.Minute

const (
	allocatePhase  = "allocate"
	podCreatePhase = "pod_create"
)

func init() {
	if err := measurement.Register(resourceClaimAllocationLatencyMeasurementName, createResourceClaimAllocationLatencyMeasurement); err != nil {
		klog.Fatalf("cannot register %v: %v", resourceClaimAllocationLatencyMeasurementName, err)
	}
}

func createResourceClaimAllocationLatencyMeasurement() measurement.Measurement {
	return &resourceClaimAllocationLatencyMeasurement{
		selector:         util.NewObjectSelector(),
		entries:          measurementutil.NewObjectTransitionTimes(resourceClaimAllocationLatencyMeasurementName),
		queue:            workqueue.NewTyped[*claimEventData](),
		podCreationTimes: make(map[string]time.Time),
	}
}

type claimEventData struct {
	obj      interface{}
	recvTime time.Time
}

type resourceClaimAllocationLatencyMeasurement struct {
	selector *util.ObjectSelector

	isRunning bool
	stopCh    chan struct{}
	queue     workqueue.TypedInterface[*claimEventData]
	client    clientset.Interface

	entries *measurementutil.ObjectTransitionTimes

	threshold       time.Duration
	perc50Threshold time.Duration
	perc90Threshold time.Duration
	perc99Threshold time.Duration

	podCreationTimes map[string]time.Time
	podCacheLock     sync.RWMutex
	podGetCalls      int64
}

func (m *resourceClaimAllocationLatencyMeasurement) Execute(cfg *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(cfg.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if err := m.selector.Parse(cfg.Params); err != nil {
			return nil, err
		}
		m.threshold, err = util.GetDurationOrDefault(cfg.Params, "threshold", defaultClaimAllocationLatencyThreshold)
		if err != nil {
			return nil, err
		}
		m.perc50Threshold, err = util.GetDurationOrDefault(cfg.Params, "perc50Threshold", m.threshold)
		if err != nil {
			return nil, err
		}
		m.perc90Threshold, err = util.GetDurationOrDefault(cfg.Params, "perc90Threshold", m.threshold)
		if err != nil {
			return nil, err
		}
		m.perc99Threshold, err = util.GetDurationOrDefault(cfg.Params, "perc99Threshold", m.threshold)
		if err != nil {
			return nil, err
		}
		return nil, m.start(cfg.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		return m.gather(cfg.ClusterFramework.GetClientSets().GetClient(), cfg.Identifier)
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
}

func (m *resourceClaimAllocationLatencyMeasurement) Dispose() {
	m.stop()
}

func (m *resourceClaimAllocationLatencyMeasurement) String() string {
	return resourceClaimAllocationLatencyMeasurementName + ": " + m.selector.String()
}

func (m *resourceClaimAllocationLatencyMeasurement) start(c clientset.Interface) error {
	if m.isRunning {
		klog.V(2).Infof("%s: measurement already running", m)
		return nil
	}

	klog.V(2).Infof("%s: starting measurement", m)
	m.isRunning = true
	m.stopCh = make(chan struct{})
	m.client = c

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			m.selector.ApplySelectors(&options)
			return c.ResourceV1beta2().ResourceClaims(m.selector.Namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			m.selector.ApplySelectors(&options)
			return c.ResourceV1beta2().ResourceClaims(m.selector.Namespace).Watch(context.TODO(), options)
		},
	}

	claimInf := informer.NewInformer(lw, m.addEvent)

	podLW := &cache.ListWatch{
		ListFunc: func(o metav1.ListOptions) (runtime.Object, error) {
			m.selector.ApplySelectors(&o)
			return c.CoreV1().Pods(m.selector.Namespace).List(context.TODO(), o)
		},
		WatchFunc: func(o metav1.ListOptions) (watch.Interface, error) {
			m.selector.ApplySelectors(&o)
			return c.CoreV1().Pods(m.selector.Namespace).Watch(context.TODO(), o)
		},
	}
	podInf := informer.NewInformer(podLW, m.addPodEvent)

	go m.processEvents()

	const informerSyncTimeout = time.Minute
	if err := informer.StartAndSync(claimInf, m.stopCh, informerSyncTimeout); err != nil {
		return err
	}
	return informer.StartAndSync(podInf, m.stopCh, informerSyncTimeout)
}

func (m *resourceClaimAllocationLatencyMeasurement) addEvent(_, obj interface{}) {
	event := &claimEventData{obj: obj, recvTime: time.Now()}
	m.queue.Add(event)
}

func (m *resourceClaimAllocationLatencyMeasurement) addPodEvent(_, obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod == nil || !usesResourceClaimTemplate(pod) {
		return
	}
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	m.podCacheLock.Lock()
	if _, exists := m.podCreationTimes[key]; !exists {
		m.podCreationTimes[key] = pod.CreationTimestamp.Time
	}
	m.podCacheLock.Unlock()
}

func (m *resourceClaimAllocationLatencyMeasurement) processEvents() {
	for m.processNextWorkItem() {
	}
}

func (m *resourceClaimAllocationLatencyMeasurement) processNextWorkItem() bool {
	item, quit := m.queue.Get()
	if quit {
		return false
	}
	defer m.queue.Done(item)

	m.processEvent(item)
	return true
}

func (m *resourceClaimAllocationLatencyMeasurement) processEvent(ev *claimEventData) {
	if ev.obj == nil {
		return
	}

	claim, ok := ev.obj.(*resourcev1beta2.ResourceClaim)
	if !ok {
		return
	}

	key := fmt.Sprintf("%s/%s", claim.Namespace, claim.Name)

	allocated := isAllocated(claim)

	// If the claim is already allocated and we haven't seen it before, ignore it.
	if allocated {
		if _, found := m.entries.Get(key, createPhase); !found {
			return
		}
	}

	// Record creation time once per claim.
	if _, found := m.entries.Get(key, createPhase); !found {
		m.entries.Set(key, createPhase, claim.CreationTimestamp.Time)
	}

	// Record allocation time when status becomes non-empty.
	if allocated {
		if _, exists := m.entries.Get(key, allocatePhase); !exists {
			m.entries.Set(key, allocatePhase, ev.recvTime)
		}
	}

	if _, found := m.entries.Get(key, podCreatePhase); !found {
		if ts, ok := m.getCachedPodCreateTime(claim); ok {
			m.entries.Set(key, podCreatePhase, ts)
		} else if ts, ok := m.fetchPodCreateTime(claim); ok {
			m.entries.Set(key, podCreatePhase, ts)
		}
	}
}

func (m *resourceClaimAllocationLatencyMeasurement) stop() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
		m.queue.ShutDown()
	}
}

func transitionsWithThreshold(th time.Duration) map[string]measurementutil.Transition {
	return map[string]measurementutil.Transition{
		"claim_allocation":           {From: createPhase, To: allocatePhase, Threshold: th},
		"pod_create_to_claim_create": {From: podCreatePhase, To: createPhase, Threshold: th},
	}
}

func (m *resourceClaimAllocationLatencyMeasurement) gather(_ clientset.Interface, identifier string) ([]measurement.Summary, error) {
	klog.V(2).Infof("%s: gathering results", m)

	if !m.isRunning {
		return nil, fmt.Errorf("measurement %s has not been started", resourceClaimAllocationLatencyMeasurementName)
	}

	m.stop()

	latencies := m.entries.CalculateTransitionsLatency(transitionsWithThreshold(m.threshold), measurementutil.MatchAll)

	var err error
	if slosErr := latencies["claim_allocation"].VerifyThresholdByPercentile(m.perc50Threshold, m.perc90Threshold, m.perc99Threshold); slosErr != nil {
		err = errors.NewMetricViolationError("claim allocation", slosErr.Error())
		klog.Errorf("%s: %v", m, err)
	}

	perf := measurementutil.LatencyMapToPerfData(latencies)
	perf.DataItems = append(perf.DataItems, measurementutil.DataItem{
		Data:   map[string]float64{"Count": float64(atomic.LoadInt64(&m.podGetCalls))},
		Unit:   "count",
		Labels: map[string]string{"Metric": "PodGetCalls"},
	})
	content, jsonErr := util.PrettyPrintJSON(perf)
	if jsonErr != nil {
		return nil, jsonErr
	}

	summaryName := fmt.Sprintf("%s_%s", resourceClaimAllocationLatencyMeasurementName, identifier)
	return []measurement.Summary{measurement.CreateSummary(summaryName, "json", content)}, err
}

func isAllocated(claim *resourcev1beta2.ResourceClaim) bool {
	return claim.Status.Allocation != nil || len(claim.Status.ReservedFor) > 0 || len(claim.Status.Devices) > 0
}

func usesResourceClaimTemplate(p *corev1.Pod) bool {
	for _, rc := range p.Spec.ResourceClaims {
		if rc.ResourceClaimTemplateName != nil && *rc.ResourceClaimTemplateName != "" {
			return true
		}
	}
	return false
}

func (m *resourceClaimAllocationLatencyMeasurement) getCachedPodCreateTime(cl *resourcev1beta2.ResourceClaim) (time.Time, bool) {
	for _, o := range cl.OwnerReferences {
		if o.Kind == "Pod" && o.Name != "" {
			key := fmt.Sprintf("%s/%s", cl.Namespace, o.Name)
			m.podCacheLock.RLock()
			ts, ok := m.podCreationTimes[key]
			m.podCacheLock.RUnlock()
			return ts, ok
		}
	}
	return time.Time{}, false
}

func (m *resourceClaimAllocationLatencyMeasurement) fetchPodCreateTime(cl *resourcev1beta2.ResourceClaim) (time.Time, bool) {
	for _, o := range cl.OwnerReferences {
		if o.Kind == "Pod" && o.Name != "" {
			atomic.AddInt64(&m.podGetCalls, 1)
			pod, err := m.client.CoreV1().Pods(cl.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return time.Time{}, false
			}
			ts := pod.CreationTimestamp.Time
			key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			m.podCacheLock.Lock()
			m.podCreationTimes[key] = ts
			m.podCacheLock.Unlock()
			return ts, true
		}
	}
	return time.Time{}, false
}

var _ measurement.Measurement = &resourceClaimAllocationLatencyMeasurement{}
