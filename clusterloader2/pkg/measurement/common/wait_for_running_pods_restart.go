/*
Copyright 2024 The Kubernetes Authors.

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
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	waitForRunningPodsRestartMeasurementName = "WaitForPodsRecovery"
)

func init() {
	if err := measurement.Register(waitForRunningPodsRestartMeasurementName, createWaitForRunningPodsRestartMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForRunningPodsRestartMeasurementName, err)
	}
}

func createWaitForRunningPodsRestartMeasurement() measurement.Measurement {
	return &waitForRunningPodsRestartMeasurement{}
}

type waitForRunningPodsRestartMeasurement struct {
	lock      sync.Mutex
	isRunning bool
	podsCount int
	selector  *util.ObjectSelector
}

// Execute supports "start", "gather", and "stop" actions.
// On "start", all pods matching the given selector are counted and saved.
// On "gather", the measurement waits until the number of pods matching the selector is within the configured range/toleration.
func (w *waitForRunningPodsRestartMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		return nil, w.start(config)
	case "gather":
		return nil, w.gather(config)
	case "stop":
		w.Dispose()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (w *waitForRunningPodsRestartMeasurement) start(config *measurement.Config) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return err
	}

	client := config.ClusterFramework.GetClientSets().GetClient()
	listOptions := metav1.ListOptions{}
	selector.ApplySelectors(&listOptions)
	podList, err := client.CoreV1().Pods(selector.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	podsCount := len(podList.Items)

	w.podsCount = podsCount
	w.selector = selector
	w.isRunning = true

	klog.V(2).Infof("%s: started, found %d total pods matching selector '%s'",
		w, podsCount, selector.String())
	return nil
}

func (w *waitForRunningPodsRestartMeasurement) gather(config *measurement.Config) error {
	w.lock.Lock()
	if !w.isRunning {
		w.lock.Unlock()
		return fmt.Errorf("measurement %s has not been started", w)
	}
	podsCount := w.podsCount
	selector := w.selector
	w.lock.Unlock()

	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPodsTimeout)
	if err != nil {
		return err
	}
	refreshInterval, err := util.GetDurationOrDefault(config.Params, "refreshInterval", defaultWaitForPodsInterval)
	if err != nil {
		return err
	}
	tolerationTimeout, err := util.GetDurationOrDefault(config.Params, "tolerationTimeout", 0)
	if err != nil {
		return err
	}
	isFatal, err := util.GetBoolOrDefault(config.Params, "isFatal", defaultIsFatal)
	if err != nil {
		return err
	}

	minDesiredPtr, maxDesiredPtr, tolerationPtr, err := measurementutil.ParseDesiredPodRange(config.Params)
	if err != nil {
		return err
	}
	minDesired, maxDesired, margin, err := measurementutil.CalculateDesiredPodRange(config.Params, podsCount)
	if err != nil {
		return err
	}

	klog.V(2).Infof("%s: waiting for %d-%d pods (initially %d, margin %d) with selector '%s'",
		w, minDesired, maxDesired, podsCount, margin, selector.String())

	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	podStore, err := measurementutil.NewPodStore(config.ClusterFramework.GetClientSets().GetClient(), selector)
	if err != nil {
		return err
	}
	defer podStore.Stop()

	checkPodsRunning := false
	options := &measurementutil.WaitForPodOptions{
		DesiredPodCount:     func() int { return podsCount },
		MinDesiredPodCount:  minDesiredPtr,
		MaxDesiredPodCount:  maxDesiredPtr,
		Toleration:          tolerationPtr,
		CallerName:          w.String(),
		WaitForPodsInterval: refreshInterval,
		TolerationTimeout:   tolerationTimeout,
		CheckPodsRunning:    &checkPodsRunning,
	}

	_, err = measurementutil.WaitForPods(ctx, podStore, options)
	if err != nil && isFatal {
		return errors.NewErrCritical(err)
	}
	return err
}

// Dispose cleans up after the measurement.
func (w *waitForRunningPodsRestartMeasurement) Dispose() {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.isRunning = false
	w.podsCount = 0
	w.selector = nil
}

// String returns a string representation of the measurement.
func (*waitForRunningPodsRestartMeasurement) String() string {
	return waitForRunningPodsRestartMeasurementName
}
