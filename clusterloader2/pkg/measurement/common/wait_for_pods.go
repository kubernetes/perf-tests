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
	"context"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForPodsTimeout         = 60 * time.Second
	defaultWaitForPodsInterval        = 5 * time.Second
	waitForRunningPodsMeasurementName = "WaitForRunningPods"
)

func init() {
	if err := measurement.Register(waitForRunningPodsMeasurementName, createWaitForRunningPodsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForRunningPodsMeasurementName, err)
	}
}

func createWaitForRunningPodsMeasurement() measurement.Measurement {
	return &waitForRunningPodsMeasurement{}
}

type waitForRunningPodsMeasurement struct{}

// Execute waits until desired number of pods are running or until timeout happens.
// Pods can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
func (w *waitForRunningPodsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	desiredPodCount, err := util.GetInt(config.Params, "desiredPodCount")
	if err != nil {
		return nil, err
	}
	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return nil, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPodsTimeout)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	options := &measurementutil.WaitForPodOptions{
		DesiredPodCount:     func() int { return desiredPodCount },
		CallerName:          w.String(),
		WaitForPodsInterval: defaultWaitForPodsInterval,
	}
	podStore, err := measurementutil.NewPodStore(config.ClusterFramework.GetClientSets().GetClient(), selector)
	if err != nil {
		return nil, err
	}
	return nil, measurementutil.WaitForPods(ctx, podStore, options)
}

// Dispose cleans up after the measurement.
func (*waitForRunningPodsMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForRunningPodsMeasurement) String() string {
	return waitForRunningPodsMeasurementName
}
