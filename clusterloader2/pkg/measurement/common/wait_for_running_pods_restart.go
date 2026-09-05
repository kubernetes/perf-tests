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
	"math"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	waitForRunningPodsRestartMeasurementName = "WaitForRunningPodsRestart"
)

func init() {
	names := []string{
		waitForRunningPodsRestartMeasurementName,
		"WaitForPodsRestart",
		"WaitForRunningPodsResync",
		"WaitForPodsResync",
		"WaitForRunningPodsRecovery",
		"WaitForPodsRecovery",
	}
	for _, name := range names {
		if err := measurement.Register(name, createWaitForRunningPodsRestartMeasurementFactory(name)); err != nil {
			klog.Fatalf("Cannot register %s: %v", name, err)
		}
	}
}

func createWaitForRunningPodsRestartMeasurementFactory(name string) func() measurement.Measurement {
	return func() measurement.Measurement {
		return &waitForRunningPodsRestartMeasurement{
			callerName: name,
		}
	}
}

type waitForRunningPodsRestartMeasurement struct {
	lock           sync.Mutex
	isRunning      bool
	totalPodsCount int
	selector       *util.ObjectSelector
	callerName     string
}

// Execute supports "start", "gather", and "stop" actions.
// On "start", all pods matching the given selector are counted and saved.
// On "gather", the measurement waits until all counted pods (within configurable % difference) are back up and Running.
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

	totalPodsCount := len(podList.Items)

	w.totalPodsCount = totalPodsCount
	w.selector = selector
	w.isRunning = true

	klog.V(2).Infof("%s: started, found %d total pods matching selector '%s'",
		w, totalPodsCount, selector.String())
	return nil
}

func (w *waitForRunningPodsRestartMeasurement) gather(config *measurement.Config) error {
	w.lock.Lock()
	if !w.isRunning {
		w.lock.Unlock()
		return fmt.Errorf("measurement %s has not been started", w)
	}
	totalPodsCount := w.totalPodsCount
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

	minDesired, maxDesired, margin, err := calculateDesiredPodRange(config.Params, totalPodsCount)
	if err != nil {
		return err
	}

	klog.V(2).Infof("%s: waiting for %d-%d pods (initially %d, margin %d) with selector '%s' to be running",
		w, minDesired, maxDesired, totalPodsCount, margin, selector.String())

	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	podStore, err := measurementutil.NewPodStore(config.ClusterFramework.GetClientSets().GetClient(), selector)
	if err != nil {
		return err
	}
	defer podStore.Stop()

	err = w.waitForPods(ctx, podStore, minDesired, maxDesired, totalPodsCount, refreshInterval, tolerationTimeout)
	if err != nil && isFatal {
		return errors.NewErrCritical(err)
	}
	return err
}

func (w *waitForRunningPodsRestartMeasurement) waitForPods(
	ctx context.Context,
	ps measurementutil.PodLister,
	minDesired, maxDesired, initialCount int,
	refreshInterval, tolerationTimeout time.Duration,
) error {
	var timeout time.Duration
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		timeout = time.Until(deadline)
	}
	klog.V(2).Infof("%s: %s: starting with timeout: %v, expecting %d-%d running pods (initially %d)",
		w, ps.String(), timeout, minDesired, maxDesired, initialCount)

	oldPods, err := ps.List()
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	oldPodsStatus := measurementutil.ComputePodsStartupStatus(oldPods, initialCount, nil)
	var tolerationCh <-chan time.Time
	if tolerationTimeout > 0 {
		timer := time.NewTimer(tolerationTimeout)
		defer timer.Stop()
		tolerationCh = timer.C
	}

	var tolerationExpired bool
	var tolerationExpiredAt time.Time

	for {
		select {
		case <-ctx.Done():
			latestPods, listErr := ps.List()
			if listErr == nil {
				oldPods = latestPods
				oldPodsStatus = measurementutil.ComputePodsStartupStatus(oldPods, initialCount, nil)
			}
			if ctx.Err() == context.DeadlineExceeded {
				notRunning := getNotRunningPods(oldPods)
				klog.V(2).Infof("%s: %s: expected %d-%d pods, got %d pods (not running pods: %s)",
					w, ps.String(), minDesired, maxDesired, len(oldPods), strings.Join(notRunning, ", "))
				klog.V(2).Infof("%s: %s: pods still not in Running state: %s",
					w, ps.String(), strings.Join(notRunning, ", "))
				if minDesired == maxDesired {
					return fmt.Errorf("got %w while waiting for %d pods to be running in %s - summary of pods : %s, not running pods: %s",
						ctx.Err(), minDesired, ps.String(), oldPodsStatus.String(), strings.Join(notRunning, ", "))
				}
				return fmt.Errorf("got %w while waiting for %d-%d pods to be running in %s - summary of pods : %s, not running pods: %s",
					ctx.Err(), minDesired, maxDesired, ps.String(), oldPodsStatus.String(), strings.Join(notRunning, ", "))
			}
			return ctx.Err()

		case <-tolerationCh:
			pods, err := ps.List()
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}
			podsStatus := measurementutil.ComputePodsStartupStatus(pods, initialCount, nil)
			klog.V(2).Infof("%s: %s: toleration timeout expired, pods status: %s", w, ps.String(), podsStatus.String())
			if isPodsStatusAcceptable(pods, podsStatus, minDesired, maxDesired) {
				return nil
			}
			notRunning := getNotRunningPods(pods)
			klog.V(2).Infof("%s: %s: toleration timeout expired, pods not in Running state: %s",
				w, ps.String(), strings.Join(notRunning, ", "))
			tolerationExpired = true
			tolerationExpiredAt = time.Now()
			oldPods = pods
			oldPodsStatus = podsStatus

		case <-time.After(refreshInterval):
			pods, err := ps.List()
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}
			podsStatus := measurementutil.ComputePodsStartupStatus(pods, initialCount, nil)

			diff := measurementutil.DiffPods(oldPods, pods)
			deletedPods := diff.DeletedPods()
			if len(oldPods) < minDesired && len(deletedPods) > 0 {
				klog.Warningf("%s: %s: %d pods disappeared: %v", w, ps.String(), len(deletedPods), strings.Join(deletedPods, ", "))
			}
			addedPods := diff.AddedPods()
			if len(oldPods) > maxDesired && len(addedPods) > 0 {
				klog.Warningf("%s: %s: %d pods appeared: %v", w, ps.String(), len(addedPods), strings.Join(addedPods, ", "))
			}
			if podsStatus.String() != oldPodsStatus.String() {
				klog.V(2).Infof("%s: %s: %s", w, ps.String(), podsStatus.String())
			}
			if isPodsStatusAcceptable(pods, podsStatus, minDesired, maxDesired) {
				if tolerationExpired {
					delay := time.Since(tolerationExpiredAt)
					if minDesired == maxDesired {
						return fmt.Errorf("desired number of %d pods in %s reached after tolerationTimeout (%v), delay after tolerationTimeout was %v",
							minDesired, ps.String(), tolerationTimeout, delay)
					}
					return fmt.Errorf("desired number of %d-%d pods in %s reached after tolerationTimeout (%v), delay after tolerationTimeout was %v",
						minDesired, maxDesired, ps.String(), tolerationTimeout, delay)
				}
				return nil
			}
			oldPods = pods
			oldPodsStatus = podsStatus
		}
	}
}

func isPodsStatusAcceptable(pods []*corev1.Pod, podsStatus measurementutil.PodsStartupStatus, minDesired, maxDesired int) bool {
	// We wait until all pods are running and ready, and the total running count is in [minDesired, maxDesired].
	if len(pods) == podsStatus.Running &&
		podsStatus.Running == podsStatus.RunningUpdated &&
		podsStatus.RunningUpdated >= minDesired && podsStatus.RunningUpdated <= maxDesired {
		return true
	}
	return false
}

func getNotRunningPods(pods []*corev1.Pod) []string {
	var notRunning []string
	for _, p := range pods {
		if !isPodRunning(p) {
			if p.Namespace != "" {
				notRunning = append(notRunning, fmt.Sprintf("%s/%s", p.Namespace, p.Name))
			} else {
				notRunning = append(notRunning, p.Name)
			}
		}
	}
	return notRunning
}

func isPodRunning(p *corev1.Pod) bool {
	if p.DeletionTimestamp != nil {
		return false
	}
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func calculateDesiredPodRange(params map[string]interface{}, initialRunningCount int) (minDesired, maxDesired, margin int, err error) {
	hasMin := false
	hasMax := false
	if minVal, err := util.GetInt(params, "minDesiredPodCount"); err == nil {
		minDesired = minVal
		hasMin = true
	}
	if maxVal, err := util.GetInt(params, "maxDesiredPodCount"); err == nil {
		maxDesired = maxVal
		hasMax = true
	}
	if hasMin && hasMax {
		if minDesired > maxDesired {
			return 0, 0, 0, fmt.Errorf("minDesiredPodCount (%d) cannot be greater than maxDesiredPodCount (%d)", minDesired, maxDesired)
		}
		margin = (maxDesired - minDesired) / 2
		return minDesired, maxDesired, margin, nil
	}

	// Check countErrorMargin (absolute pod count margin)
	countErrorMargin, err := util.GetIntOrDefault(params, "countErrorMargin", 0)
	if err != nil {
		return 0, 0, 0, err
	}
	if countErrorMargin > 0 {
		margin = countErrorMargin
	}

	// Check allowedDifferencePercentage / tolerancePercentage / tolerationPercentage / differencePercentage
	percentage, err := util.GetFloat64OrDefault(params, "allowedDifferencePercentage", 0.0)
	if err != nil {
		return 0, 0, 0, err
	}
	if percentage == 0.0 {
		percentage, err = util.GetFloat64OrDefault(params, "tolerancePercentage", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if percentage == 0.0 {
		percentage, err = util.GetFloat64OrDefault(params, "tolerationPercentage", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if percentage == 0.0 {
		percentage, err = util.GetFloat64OrDefault(params, "differencePercentage", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if percentage > 0.0 {
		percentMargin := int(math.Ceil(float64(initialRunningCount) * percentage / 100.0))
		if percentMargin > margin {
			margin = percentMargin
		}
	}

	// Check allowedDifferenceRatio / tolerationRatio / toleranceRatio
	ratio, err := util.GetFloat64OrDefault(params, "allowedDifferenceRatio", 0.0)
	if err != nil {
		return 0, 0, 0, err
	}
	if ratio == 0.0 {
		ratio, err = util.GetFloat64OrDefault(params, "tolerationRatio", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if ratio == 0.0 {
		ratio, err = util.GetFloat64OrDefault(params, "toleranceRatio", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if ratio > 0.0 {
		ratioMargin := int(math.Ceil(float64(initialRunningCount) * ratio))
		if ratioMargin > margin {
			margin = ratioMargin
		}
	}

	// Check general tolerance / toleration float parameter
	tolerance, err := util.GetFloat64OrDefault(params, "tolerance", 0.0)
	if err != nil {
		return 0, 0, 0, err
	}
	if tolerance == 0.0 {
		tolerance, err = util.GetFloat64OrDefault(params, "toleration", 0.0)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if tolerance > 0.0 {
		var tolMargin int
		if tolerance <= 1.0 {
			tolMargin = int(math.Ceil(float64(initialRunningCount) * tolerance))
		} else {
			tolMargin = int(math.Ceil(float64(initialRunningCount) * tolerance / 100.0))
		}
		if tolMargin > margin {
			margin = tolMargin
		}
	}

	computedMin := initialRunningCount - margin
	if computedMin < 0 {
		computedMin = 0
	}
	computedMax := initialRunningCount + margin

	if hasMin {
		minDesired = minDesired
	} else {
		minDesired = computedMin
	}

	if hasMax {
		maxDesired = maxDesired
	} else {
		maxDesired = computedMax
	}

	if minDesired > maxDesired {
		return 0, 0, 0, fmt.Errorf("minDesiredPodCount (%d) cannot be greater than maxDesiredPodCount (%d)", minDesired, maxDesired)
	}

	return minDesired, maxDesired, margin, nil
}

// Dispose cleans up after the measurement.
func (w *waitForRunningPodsRestartMeasurement) Dispose() {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.isRunning = false
	w.totalPodsCount = 0
	w.selector = nil
}

// String returns a string representation of the measurement.
func (w *waitForRunningPodsRestartMeasurement) String() string {
	return w.callerName
}
