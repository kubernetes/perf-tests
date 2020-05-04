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

package common

import (
	"time"

	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	sleepName = "Sleep"
)

func init() {
	if err := measurement.Register(sleepName, createSleepMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", sleepName, err)
	}
}

func createSleepMeasurement() measurement.Measurement {
	return &sleepMeasurement{}
}

type sleepMeasurement struct{}

func (s *sleepMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	durationStr, err := util.GetString(config.Params, "duration")
	if err != nil {
		return nil, err
	}
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, err
	}
	time.Sleep(duration)
	return nil, nil
}

func (s *sleepMeasurement) Dispose() {}

func (s *sleepMeasurement) String() string { return sleepName }
