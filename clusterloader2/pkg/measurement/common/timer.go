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
	"sync"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	timerMeasurementName = "Timer"
)

func init() {
	if err := measurement.Register(timerMeasurementName, createTimerMeasurment); err != nil {
		klog.Fatalf("Cannot register %s: %v", timerMeasurementName, err)
	}
}

func createTimerMeasurment() measurement.Measurement {
	return &timer{
		startTimes: make(map[string]time.Time),
		durations:  make(map[string]time.Duration),
	}
}

type timer struct {
	lock       sync.Mutex
	startTimes map[string]time.Time
	durations  map[string]time.Duration
}

// Execute supports two actions. start - which start timer. stop - which stops timer
// and collects time duration between start and stop.
// Both start and stop actions require label parameter to be provided.
// Gather action logs a measurement for all collected phases durations.
func (t *timer) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	t.lock.Lock()
	defer t.lock.Unlock()
	switch action {
	case "start":
		label, err := util.GetString(config.Params, "label")
		if err != nil {
			return nil, err
		}
		t.startTimes[label] = time.Now()
	case "stop":
		label, err := util.GetString(config.Params, "label")
		if err != nil {
			return nil, err
		}
		startTime, ok := t.startTimes[label]
		if !ok {
			return nil, fmt.Errorf("uninitialized timer %s", label)
		}
		duration := time.Since(startTime)
		klog.V(0).Infof("%s: %s - %v", t, label, duration)
		t.durations[label] = duration
		delete(t.startTimes, label)
	case "gather":
		result := measurementutil.PerfData{
			Version: "v1",
			DataItems: []measurementutil.DataItem{{
				Unit:   "s",
				Labels: map[string]string{"test": "phases"},
				Data:   make(map[string]float64)}}}

		for label, duration := range t.durations {
			result.DataItems[0].Data[label] = duration.Seconds()
		}
		content, err := util.PrettyPrintJSON(result)
		if err != nil {
			return nil, err
		}
		summary := measurement.CreateSummary(timerMeasurementName, "json", content)
		return []measurement.Summary{summary}, nil
	default:
		return nil, fmt.Errorf("unknown action %s", action)
	}
	return nil, nil
}

// Dispose cleans up after the measurement.
func (t *timer) Dispose() {}

// String returns string representation of this measurement.
func (*timer) String() string {
	return timerMeasurementName
}
