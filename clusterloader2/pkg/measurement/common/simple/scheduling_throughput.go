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

package simple

import (
	"fmt"
	"math"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulingThroughputMeasurementName = "SchedulingThroughput"
)

func init() {
	measurement.Register(schedulingThroughputMeasurementName, createSchedulingThroughputMeasurement)
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
func (s *schedulingThroughputMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}
	switch action {
	case "start":
		if s.isRunning {
			klog.Infof("%s: measurement already running", s)
			return summaries, nil
		}
		namespace, err := util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
		if err != nil {
			return summaries, err
		}
		labelSelector, err := util.GetStringOrDefault(config.Params, "labelSelector", "")
		if err != nil {
			return summaries, err
		}
		fieldSelector, err := util.GetStringOrDefault(config.Params, "fieldSelector", "")
		if err != nil {
			return summaries, err
		}

		s.stopCh = make(chan struct{})
		return summaries, s.start(config.ClusterFramework.GetClientSets().GetClient(), namespace, labelSelector, fieldSelector)
	case "gather":
		return s.gather()
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
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

func (s *schedulingThroughputMeasurement) start(clientSet clientset.Interface, namespace, labelSelector, fieldSelector string) error {
	ps, err := measurementutil.NewPodStore(clientSet, namespace, labelSelector, fieldSelector)
	if err != nil {
		return fmt.Errorf("pod store creation error: %v", err)
	}
	s.isRunning = true
	klog.Infof("%s: starting collecting throughput data", s)

	go func() {
		defer ps.Stop()
		selectorsString := measurementutil.CreateSelectorsString(namespace, labelSelector, fieldSelector)
		lastScheduledCount := 0
		for {
			select {
			case <-s.stopCh:
				return
			case <-time.After(defaultWaitForPodsInterval):
				pods := ps.List()
				podsStatus := measurementutil.ComputePodsStartupStatus(pods, 0)
				throughput := float64(podsStatus.Scheduled-lastScheduledCount) / float64(defaultWaitForPodsInterval/time.Second)
				s.schedulingThroughputs = append(s.schedulingThroughputs, throughput)
				lastScheduledCount = podsStatus.Scheduled
				klog.Infof("%v: %s: %d pods scheduled", s, selectorsString, lastScheduledCount)
			}
		}
	}()
	return nil
}

func (s *schedulingThroughputMeasurement) gather() ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	if !s.isRunning {
		klog.Errorf("%s: measurementis nor running", s)
		return summaries, fmt.Errorf("measurement is not running")
	}
	s.stop()
	klog.Infof("%s: gathering data", s)

	summary := &schedulingThroughput{}
	if length := len(s.schedulingThroughputs); length > 0 {
		if length == 0 {
			summaries = append(summaries, summary)
			return summaries, nil
		}

		sort.Float64s(s.schedulingThroughputs)
		sum := 0.0
		for i := range s.schedulingThroughputs {
			sum += s.schedulingThroughputs[i]
		}
		summary.Average = sum / float64(length)
		summary.Perc50 = s.schedulingThroughputs[int(math.Ceil(float64(length*50)/100))-1]
		summary.Perc90 = s.schedulingThroughputs[int(math.Ceil(float64(length*90)/100))-1]
		summary.Perc99 = s.schedulingThroughputs[int(math.Ceil(float64(length*99)/100))-1]
	}
	summaries = append(summaries, summary)
	return summaries, nil
}

func (s *schedulingThroughputMeasurement) stop() {
	if s.isRunning {
		close(s.stopCh)
		s.isRunning = false
	}
}

type schedulingThroughput struct {
	Average float64 `json:"average"`
	Perc50  float64 `json:"perc50"`
	Perc90  float64 `json:"perc90"`
	Perc99  float64 `json:"perc99"`
}

// SummaryName returns name of the summary.
func (*schedulingThroughput) SummaryName() string {
	return schedulingThroughputMeasurementName
}

// SummaryTime returns time when summary was created.
func (*schedulingThroughput) SummaryTime() time.Time {
	return time.Now()
}

// PrintSummary returns summary as a string.
func (s *schedulingThroughput) PrintSummary() (string, error) {
	return util.PrettyPrintJSON(s)
}
