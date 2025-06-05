/*
Copyright 2025 The Kubernetes Authors.

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
	"errors"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	scaleNodesMeasurementName = "ScaleNodes"
	defaultScalingTimeout     = 30 * time.Minute
	nodeCountCheckInterval    = 30 * time.Second
)

type scaleNodesMeasurement struct{}

func init() {
	if err := measurement.Register(scaleNodesMeasurementName, createScaleNodesMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", scaleNodesMeasurementName, err)
	}
}

func createScaleNodesMeasurement() measurement.Measurement {
	return &scaleNodesMeasurement{}
}

// Execute performs the node scaling operation with the specified parameters
func (n *scaleNodesMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	// Get parameters from config.Params
	providerName, err := util.GetString(config.Params, "provider")
	if err != nil {
		return nil, err
	}
	region, err := util.GetString(config.Params, "region")
	if err != nil {
		return nil, err
	}
	clusterName, err := util.GetString(config.Params, "clusterName")
	if err != nil {
		return nil, err
	}
	batchSize, err := util.GetInt(config.Params, "batchSize")
	if err != nil {
		return nil, err
	}
	intervalSeconds, err := util.GetInt(config.Params, "intervalSeconds")
	if err != nil {
		return nil, err
	}
	targetNodeCount, err := util.GetInt(config.Params, "targetNodeCount")
	if err != nil {
		return nil, err
	}

	// Get timeout with default value if not specified
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultScalingTimeout)
	if err != nil {
		return nil, err
	}

	// Initialize provider specific scaler
	scaler, err := provider.CreateNodeScaler(providerName, region, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to create node scaler: %v", err)
	}

	// Start scaling operation
	klog.Infof("Starting node scaling: target=%d, batchSize=%d/interval, interval=%ds, timeout=%v",
		targetNodeCount, batchSize, intervalSeconds, timeout)

	// Start the scaling operation in a goroutine
	errCh := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	go func() {
		errCh <- scaler.ScaleNodes(ctx, batchSize, intervalSeconds, targetNodeCount)
	}()

	// Create stop channel for timeout
	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})

	// Set up options for waiting on nodes
	options := &measurementutil.WaitForNodeOptions{
		Selector:             util.NewObjectSelector(),
		MinDesiredNodeCount:  targetNodeCount,
		MaxDesiredNodeCount:  targetNodeCount,
		CallerName:           n.String(),
		WaitForNodesInterval: nodeCountCheckInterval,
	}

	// Wait for either the scaling operation to fail or nodes to be ready
	select {
	case err := <-errCh:
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("scaling operation timed out after %v", timeout)
			}
			return nil, fmt.Errorf("failed to scale nodes: %v", err)
		}
		// Scaling operation completed, now wait for nodes to be ready
		if err := measurementutil.WaitForNodes(config.ClusterFramework.GetClientSets().GetClient(), stopCh, options); err != nil {
			return nil, err
		}
		return nil, nil
	case <-stopCh:
		return nil, fmt.Errorf("timeout while waiting for scaling operation to complete after %v", timeout)
	}
}

// Dispose cleans up after the measurement.
func (*scaleNodesMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*scaleNodesMeasurement) String() string {
	return scaleNodesMeasurementName
}
