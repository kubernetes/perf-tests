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
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForNodesTimeout  = 30 * time.Minute
	defaultWaitForNodesInterval = 30 * time.Second
	waitForNodesMeasurementName = "WaitForNodes"
)

func init() {
	if err := measurement.Register(waitForNodesMeasurementName, createWaitForNodesMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForNodesMeasurementName, err)
	}
}

func createWaitForNodesMeasurement() measurement.Measurement {
	return &waitForNodesMeasurement{}
}

type waitForNodesMeasurement struct{}

// Execute waits until desired number of Nodes are ready or until a
// timeout happens. Nodes can be optionally specified by field and/or label
// selectors.
func (w *waitForNodesMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	minNodeCount, maxNodeCount, err := getMinMaxDesiredNodeCount(config.Params)
	if err != nil {
		return nil, err
	}

	selector := measurementutil.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return nil, err
	}

	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForNodesTimeout)
	if err != nil {
		return nil, err
	}
	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})

	options := &measurementutil.WaitForNodeOptions{
		Selector:             selector,
		MinDesiredNodeCount:  minNodeCount,
		MaxDesiredNodeCount:  maxNodeCount,
		EnableLogging:        true,
		CallerName:           w.String(),
		WaitForNodesInterval: defaultWaitForNodesInterval,
	}
	return nil, measurementutil.WaitForNodes(config.ClusterFramework.GetClientSets().GetClient(), stopCh, options)
}

// Dispose cleans up after the measurement.
func (*waitForNodesMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForNodesMeasurement) String() string {
	return waitForNodesMeasurementName
}

func getMinMaxDesiredNodeCount(params map[string]interface{}) (minDesiredNodeCount, maxDesiredNodeCount int, err error) {
	minDesiredNodeCount, err = util.GetInt(params, "minDesiredNodeCount")
	if err != nil {
		return
	}
	maxDesiredNodeCount, err = util.GetInt(params, "maxDesiredNodeCount")
	return
}
