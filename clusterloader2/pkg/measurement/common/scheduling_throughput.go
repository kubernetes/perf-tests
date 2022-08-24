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

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulingThroughputMeasurementName = "SchedulingThroughput"
	defaultSchedulingThroughputInterval = 5 * time.Second
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
	schedulingThroughputs []float64
	isRunning             bool
	stopCh                chan struct{}
}

// Execute supports two actions:
// - start - starts the pods scheduling observation.
//   Pods can be specified by field and/or label selectors.
//   If namespace is not passed by parameter, all-namespace scope is assumed.
// - gather - creates summary for observed values.
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
		selector := util.NewObjectSelector()
		if err := selector.Parse(config.Params); err != nil {
			return nil, err
		}
		measurmentInterval, err := util.GetDurationOrDefault(config.Params, "measurmentInterval", defaultSchedulingThroughputInterval)
		if err != nil {
			return nil, err
		}
		s.stopCh = make(chan struct{})
		return nil, s.start(config.ClusterFramework.GetClientSets().GetClient(), selector, measurmentInterval)
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

// Dispose cleans up after the measurement.
func (s *schedulingThroughputMeasurement) Dispose() {
	s.stop()
}

// String returns a string representation of the measurement.
func (*schedulingThroughputMeasurement) String() string {
	return schedulingThroughputMeasurementName
}

func (s *schedulingThroughputMeasurement) start(clientSet clientset.Interface, selector *util.ObjectSelector, measurmentInterval time.Duration) error {
	ps, err := measurementutil.NewPodStore(clientSet, selector)
	if err != nil {
		return fmt.Errorf("pod store creation error: %v", err)
	}
	s.isRunning = true
	klog.V(2).Infof("%s: starting collecting throughput data", s)

	go func() {
		defer ps.Stop()
		lastScheduledCount := 0
		for {
			select {
			case <-s.stopCh:
				return
			case <-time.After(measurmentInterval):
				pods, err := ps.List()
				if err != nil {
					// List in NewPodStore never returns error.
					// TODO(mborsz): Even if this is a case now, it doesn't need to be true in future. Refactor this.
					panic(fmt.Errorf("unexpected error on PodStore.List: %w", err))
				}
				podsStatus := measurementutil.ComputePodsStartupStatus(pods, 0, nil /* updatePodPredicate */)
				throughput := float64(podsStatus.Scheduled-lastScheduledCount) / float64(measurmentInterval/time.Second)
				s.schedulingThroughputs = append(s.schedulingThroughputs, throughput)
				lastScheduledCount = podsStatus.Scheduled
				klog.V(3).Infof("%v: %s: %d pods scheduled", s, selector.String(), lastScheduledCount)
			}
		}
	}()
	return nil
}

func (s *schedulingThroughputMeasurement) gather(threshold float64) ([]measurement.Summary, error) {
	if !s.isRunning {
		klog.Errorf("%s: measurement is not running", s)
		return nil, fmt.Errorf("measurement is not running")
	}
	s.stop()
	klog.V(2).Infof("%s: gathering data", s)

	throughputSummary := &schedulingThroughput{}
	if length := len(s.schedulingThroughputs); length > 0 {
		sort.Float64s(s.schedulingThroughputs)
		throughputSummary.Perc50 = s.schedulingThroughputs[int(math.Ceil(float64(length*50)/100))-1]
		throughputSummary.Perc90 = s.schedulingThroughputs[int(math.Ceil(float64(length*90)/100))-1]
		throughputSummary.Perc99 = s.schedulingThroughputs[int(math.Ceil(float64(length*99)/100))-1]
		throughputSummary.Max = s.schedulingThroughputs[length-1]
	}
	content, err := util.PrettyPrintJSON(throughputSummary)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(schedulingThroughputMeasurementName, "json", content)
	if threshold > 0 && throughputSummary.Max < threshold {
		err = errors.NewMetricViolationError(
			"scheduler throughput",
			fmt.Sprintf("actual throughput %f lower than threshold %f", throughputSummary.Max, threshold))
	}
	return []measurement.Summary{summary}, err
}

func (s *schedulingThroughputMeasurement) stop() {
	if s.isRunning {
		close(s.stopCh)
		s.isRunning = false
	}
}

type schedulingThroughput struct {
	Perc50 float64 `json:"perc50"`
	Perc90 float64 `json:"perc90"`
	Perc99 float64 `json:"perc99"`
	Max    float64 `json:"max"`
}
