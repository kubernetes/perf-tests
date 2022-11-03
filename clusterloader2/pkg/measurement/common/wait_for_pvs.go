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
	defaultWaitForPVsTimeout           = 60 * time.Second
	defaultWaitForPVsInterval          = 5 * time.Second
	waitForAvailablePVsMeasurementName = "WaitForAvailablePVs"
)

func init() {
	if err := measurement.Register(waitForAvailablePVsMeasurementName, createWaitForAvailablePVsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForAvailablePVsMeasurementName, err)
	}
}

func createWaitForAvailablePVsMeasurement() measurement.Measurement {
	return &waitForAvailablePVsMeasurement{}
}

type waitForAvailablePVsMeasurement struct{}

// Execute waits until desired number of PVs are Available or until timeout happens.
// PVs can be specified by field and/or label selectors.
func (w *waitForAvailablePVsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	desiredPVCount, err := util.GetInt(config.Params, "desiredPVCount")
	if err != nil {
		return nil, err
	}
	provisioner, err := util.GetString(config.Params, "provisioner")
	if err != nil {
		return nil, err
	}
	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return nil, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPVsTimeout)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})
	options := &measurementutil.WaitForPVOptions{
		Selector:           selector,
		DesiredPVCount:     desiredPVCount,
		Provisioner:        provisioner,
		CallerName:         w.String(),
		WaitForPVsInterval: defaultWaitForPVsInterval,
	}
	return nil, measurementutil.WaitForPVs(config.ClusterFramework.GetClientSets().GetClient(), stopCh, options)
}

// Dispose cleans up after the measurement.
func (*waitForAvailablePVsMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForAvailablePVsMeasurement) String() string {
	return waitForAvailablePVsMeasurementName
}
