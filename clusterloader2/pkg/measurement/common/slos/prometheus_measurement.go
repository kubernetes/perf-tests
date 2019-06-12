/*
Copyright 2019 The Kubernetes Authors.

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

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func createPrometheusMeasurement(name string, gatherer Gatherer) measurement.Measurement {
	return &prometheusMeasurement{
		name:     name,
		gatherer: gatherer,
	}
}

// Gatherer is an interface for measurements based on Prometheus metrics. Those measurments don't require any preparation.
// It's assumed Prometheus is up, running and instructed to scrape required metrics in the test cluster
// (please see clusterloader2/pkg/prometheus/manifests).
type Gatherer interface {
	Gather(config *measurement.MeasurementConfig, startTime time.Time) (measurement.Summary, error)
}

type prometheusMeasurement struct {
	name     string
	gatherer Gatherer

	startTime time.Time
}

func (m *prometheusMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	if config.PrometheusFramework == nil {
		klog.Warning("%s: Prometheus is disable, skipping the measurement!", m)
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		klog.Infof("%s has started", m)
		m.startTime = time.Now()
		return nil, nil
	case "gather":
		summary, err := m.gatherer.Gather(config, m.startTime)
		if err != nil && !errors.IsMetricViolationError(err) {
			return nil, err
		}
		return []measurement.Summary{summary}, err
	default:
		return nil, fmt.Errorf("unknown action: %v", action)
	}
}

func (m *prometheusMeasurement) Dispose() {}

func (m *prometheusMeasurement) String() string {
	return m.name
}
