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
	"time"

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

var defaultResourceClaimWorkers = 1

const allocatePhase = "allocate"

func init() {
	if err := measurement.Register(resourceClaimAllocationLatencyMeasurementName, createResourceClaimAllocationLatencyMeasurement); err != nil {
		klog.Fatalf("cannot register %v: %v", resourceClaimAllocationLatencyMeasurementName, err)
	}
}

func createResourceClaimAllocationLatencyMeasurement() measurement.Measurement {
	return &resourceClaimAllocationLatencyMeasurement{
		selector: util.NewObjectSelector(),
		entries:  measurementutil.NewObjectTransitionTimes(resourceClaimAllocationLatencyMeasurementName),
		queue:    workqueue.New(),
		workers:  defaultResourceClaimWorkers,
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
	queue     *workqueue.Type

	entries *measurementutil.ObjectTransitionTimes

	threshold       time.Duration
	perc50Threshold time.Duration
	perc90Threshold time.Duration
	perc99Threshold time.Duration

	workers int
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
		m.workers, err = util.GetIntOrDefault(cfg.Params, "workers", defaultResourceClaimWorkers)
		if err != nil {
			return nil, err
		}
		if m.workers < 1 {
			m.workers = defaultResourceClaimWorkers
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

	inf := informer.NewInformer(lw, m.addEvent)
	for w := 0; w < m.workers; w++ {
		go m.processEvents()
	}

	const informerSyncTimeout = time.Minute
	return informer.StartAndSync(inf, m.stopCh, informerSyncTimeout)
}

func (m *resourceClaimAllocationLatencyMeasurement) addEvent(_, obj interface{}) {
	event := &claimEventData{obj: obj, recvTime: time.Now()}
	m.queue.Add(event)
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

	ev, ok := item.(*claimEventData)
	if !ok {
		klog.Warningf("%s: couldn't convert work item %#v", m, item)
		return true
	}
	m.processEvent(ev)
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
}

func (m *resourceClaimAllocationLatencyMeasurement) stop() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
		m.queue.ShutDown()
	}
}

func transitionsWithThreshold(th time.Duration) map[string]measurementutil.Transition {
	tr := measurementutil.Transition{From: createPhase, To: allocatePhase, Threshold: th}
	return map[string]measurementutil.Transition{"claim_allocation": tr}
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

	content, jsonErr := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(latencies))
	if jsonErr != nil {
		return nil, jsonErr
	}

	summaryName := fmt.Sprintf("%s_%s", resourceClaimAllocationLatencyMeasurementName, identifier)
	return []measurement.Summary{measurement.CreateSummary(summaryName, "json", content)}, err
}

func isAllocated(claim *resourcev1beta2.ResourceClaim) bool {
	return claim.Status.Allocation != nil || len(claim.Status.ReservedFor) > 0 || len(claim.Status.Devices) > 0
}

var _ measurement.Measurement = &resourceClaimAllocationLatencyMeasurement{}
