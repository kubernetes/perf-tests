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

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForPVCsTimeout       = 60 * time.Second
	defaultWaitForPVCsInterval      = 5 * time.Second
	waitForBoundPVCsMeasurementName = "WaitForBoundPVCs"
)

func init() {
	if err := measurement.Register(waitForBoundPVCsMeasurementName, createWaitForBoundPVCsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForBoundPVCsMeasurementName, err)
	}
}

func createWaitForBoundPVCsMeasurement() measurement.Measurement {
	return &waitForBoundPVCsMeasurement{}
}

type waitForBoundPVCsMeasurement struct{}

// Execute waits until desired number of PVCs are bound or until timeout happens.
// PVCs can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
func (w *waitForBoundPVCsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	desiredPVCCount, err := util.GetInt(config.Params, "desiredPVCCount")
	if err != nil {
		return nil, err
	}
	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return nil, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPVCsTimeout)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})
	options := &measurementutil.WaitForPVCOptions{
		Selector:            selector,
		DesiredPVCCount:     desiredPVCCount,
		CallerName:          w.String(),
		WaitForPVCsInterval: defaultWaitForPVCsInterval,
	}
	return nil, measurementutil.WaitForPVCs(config.ClusterFramework.GetClientSets().GetClient(), stopCh, options)
}

// Dispose cleans up after the measurement.
func (*waitForBoundPVCsMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForBoundPVCsMeasurement) String() string {
	return waitForBoundPVCsMeasurementName
}
