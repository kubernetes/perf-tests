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
	"fmt"
	"math"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulingThroughputMeasurementName = "SchedulingThroughput"
)

func init() {
	if err := measurement.Register(schedulingThroughputMeasurementName, createSchedulingThroughputMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", schedulingThroughputMeasurementName, err)
	}
}

func createSchedulingThroughputMeasurement() measurement.Measurement {
	return &schedulingThroughputMeasurement{}
}

type schedulingThroughputMeasurement struct {
	podStore    *measurementutil.PodStore
	selector    *util.ObjectSelector
	startSecond int64
	isRunning   bool
}

// Execute supports two actions:
//   - start - begins watching pods matching the field and/or label selectors.
//     If namespace is not passed by parameter, all-namespace scope is assumed.
//   - gather - buckets the matched pods into one-second bins by the time the
//     scheduler stamped their PodScheduled condition and reports the per-second
//     throughput distribution.
//
// Throughput is derived from each pod's PodScheduled LastTransitionTime rather
// than by polling pod counts on the client's clock. That places every bind in
// the second it actually happened, so the per-second curve is accurate
// regardless of test size or client watch-delivery lag. Because it reads only
// the pod object it works on any cluster, including managed control planes
// whose scheduler /metrics endpoint is unreachable. PodScheduled
// LastTransitionTime is second-resolution, so this is a per-second signal and
// must not be used to derive sub-second latency.
func (s *schedulingThroughputMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		if s.isRunning {
			klog.V(3).Infof("%s: measurement already running", s)
			return nil, nil
		}
		s.selector = util.NewObjectSelector()
		if err := s.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		ps, err := measurementutil.NewPodStore(config.ClusterFramework.GetClientSets().GetClient(), s.selector)
		if err != nil {
			return nil, fmt.Errorf("pod store creation error: %v", err)
		}
		s.podStore = ps
		s.startSecond = time.Now().Unix()
		s.isRunning = true
		klog.V(2).Infof("%s: started watching %s", s, s.selector.String())
		return nil, nil
	case "gather":
		threshold, err := util.GetFloat64OrDefault(config.Params, "threshold", 0)
		if err != nil {
			klog.Warningf("error while getting threshold param: %v", err)
		}
		enableViolations, err := util.GetBoolOrDefault(config.Params, "enableViolations", true)
		if err != nil {
			klog.Warningf("error while getting enableViolations param: %v", err)
		}
		summary, err := s.gather(threshold)
		if err != nil {
			if !errors.IsMetricViolationError(err) {
				klog.Errorf("%s gathering error: %v", config.Identifier, err)
				return nil, err
			}
			if !enableViolations {
				err = nil
			}
		}
		return summary, err
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (s *schedulingThroughputMeasurement) gather(threshold float64) ([]measurement.Summary, error) {
	if !s.isRunning {
		klog.Errorf("%s: measurement is not running", s)
		return nil, fmt.Errorf("measurement is not running")
	}
	pods, err := s.podStore.List()
	if err != nil {
		return nil, fmt.Errorf("unexpected error on PodStore.List: %w", err)
	}
	s.stop()

	summary := computeSchedulingThroughput(pods, s.startSecond)

	content, err := util.PrettyPrintJSON(summary)
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("%s: %d pods scheduled over %ds; peak %.0f/s, p50 %.0f/s", s, summary.TotalScheduled, summary.WindowSeconds, summary.Max, summary.Perc50)
	summaries := []measurement.Summary{measurement.CreateSummary(schedulingThroughputMeasurementName, "json", content)}
	if threshold > 0 && summary.Max < threshold {
		return summaries, errors.NewMetricViolationError(
			"scheduler throughput",
			fmt.Sprintf("actual throughput %f lower than threshold %f", summary.Max, threshold))
	}
	return summaries, nil
}

func (s *schedulingThroughputMeasurement) stop() {
	if s.isRunning {
		s.isRunning = false
		if s.podStore != nil {
			s.podStore.Stop()
		}
	}
}

// Dispose cleans up after the measurement.
func (s *schedulingThroughputMeasurement) Dispose() {
	s.stop()
}

// String returns a string representation of the measurement.
func (*schedulingThroughputMeasurement) String() string {
	return schedulingThroughputMeasurementName
}

// computeSchedulingThroughput buckets each scheduled pod into the second its
// PodScheduled condition was stamped and returns the per-second throughput
// distribution. Pods scheduled before startSecond (or not yet scheduled) are
// ignored, so a reused selector cannot pull in pre-existing pods. The active
// window is densified, filling idle seconds with zero, so the percentiles
// describe the whole scheduling window.
func computeSchedulingThroughput(pods []*corev1.Pod, startSecond int64) *schedulingThroughput {
	counts := make(map[int64]int)
	total := 0
	var minSec, maxSec int64
	first := true
	for _, pod := range pods {
		sec, ok := podScheduledSecond(pod)
		if !ok || sec < startSecond {
			continue
		}
		counts[sec]++
		total++
		if first || sec < minSec {
			minSec = sec
		}
		if first || sec > maxSec {
			maxSec = sec
		}
		first = false
	}

	summary := &schedulingThroughput{TotalScheduled: total}
	if total == 0 {
		return summary
	}
	var perSecondCounts []float64
	for sec := minSec; sec <= maxSec; sec++ {
		c := counts[sec]
		summary.PerSecond = append(summary.PerSecond, throughputPoint{UnixSecond: sec, Scheduled: c})
		perSecondCounts = append(perSecondCounts, float64(c))
	}
	summary.WindowSeconds = int(maxSec-minSec) + 1
	sorted := make([]float64, len(perSecondCounts))
	copy(sorted, perSecondCounts)
	sort.Float64s(sorted)
	summary.Perc50 = percentileOf(sorted, 50)
	summary.Perc90 = percentileOf(sorted, 90)
	summary.Perc99 = percentileOf(sorted, 99)
	summary.Max = sorted[len(sorted)-1]
	return summary
}

// podScheduledSecond returns the unix second of the pod's PodScheduled=True
// transition, or false if the pod has not been scheduled.
func podScheduledSecond(pod *corev1.Pod) (int64, bool) {
	for i := range pod.Status.Conditions {
		c := &pod.Status.Conditions[i]
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionTrue {
			return c.LastTransitionTime.Unix(), true
		}
	}
	return 0, false
}

func percentileOf(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted)*p)/100)) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

type throughputPoint struct {
	UnixSecond int64 `json:"unixSecond"`
	Scheduled  int   `json:"scheduled"`
}

type schedulingThroughput struct {
	Perc50         float64           `json:"perc50"`
	Perc90         float64           `json:"perc90"`
	Perc99         float64           `json:"perc99"`
	Max            float64           `json:"max"`
	TotalScheduled int               `json:"totalScheduled"`
	WindowSeconds  int               `json:"windowSeconds"`
	PerSecond      []throughputPoint `json:"perSecond"`
}
