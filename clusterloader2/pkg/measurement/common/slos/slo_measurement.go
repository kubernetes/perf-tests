/*
Copyright 2020 The Kubernetes Authors.

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
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	sloMeasurementName = "SLOMeasurement"
)

var sloMeasurementsNames = []string{"DnsLookupLatency", "SchedulingThroughputPrometheus"}

func init() {
	if err := measurement.Register(sloMeasurementName, createSLOMeasurements); err != nil {
		klog.Fatalf("Cannot register %s: %v", sloMeasurementName, err)
	}
}

func createSLOMeasurements() measurement.Measurement {
	sloMeasurement := &sloMeasurement{}
	for _, name := range sloMeasurementsNames {
		m, err := measurement.CreateMeasurement(name)
		if err != nil {
			klog.Fatalf("Cannot create instance of %s: %v", name, err)
		}
		sloMeasurement.measurements = append(sloMeasurement.measurements, m)
	}
	return sloMeasurement
}

type sloMeasurement struct {
	measurements []measurement.Measurement
	summaries    []measurement.Summary
}

func (s sloMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	errList := errors.NewErrorList()
	for _, m := range s.measurements {
		measurementConfig, err := getMeasurementConfig(config, m.String())
		if err != nil {
			errList.Append(err)
			continue
		}

		summaries, err := m.Execute(measurementConfig)
		if err != nil {
			errList.Append(err)
			continue
		}
		s.summaries = append(s.summaries, summaries...)
	}

	if !errList.IsEmpty() {
		return nil, errList
	}
	return s.summaries, nil
}

func getMeasurementConfig(config *measurement.Config, measurementName string) (*measurement.Config, error) {
	measurementConfig := *config
	measurementConfig.Params = util.CloneMap(config.Params)

	overrides, err := util.GetMap(config.Params, "overrides")
	if util.IsErrKeyNotFound(err) {
		return &measurementConfig, nil
	}
	if err != nil {
		return nil, err
	}

	measurementOverrides, err := util.GetMap(overrides, measurementName)
	if util.IsErrKeyNotFound(err) {
		return &measurementConfig, nil
	}
	if err != nil {
		return nil, err
	}
	for k, v := range measurementOverrides {
		measurementConfig.Params[k] = v
	}

	return &measurementConfig, nil
}

func (s sloMeasurement) Dispose() {
	for _, m := range s.measurements {
		m.Dispose()
	}
}

func (s sloMeasurement) String() string {
	return sloMeasurementName
}
