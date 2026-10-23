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

package util

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

type scalingFormat int

const (
	uninitialized scalingFormat = iota
	up
	down
	none
)

// WaitForPodOptions is an options used by WaitForPods methods.
type WaitForPodOptions struct {
	DesiredPodCount     func() int
	MinDesiredPodCount  *int
	MaxDesiredPodCount  *int
	Toleration          *float64
	CountErrorMargin    int
	CallerName          string
	WaitForPodsInterval time.Duration
	TolerationTimeout   time.Duration
	CheckPodsRunning    *bool

	// IsPodUpdated can be used to detect which pods have been already updated.
	// nil value means all pods are updated.
	IsPodUpdated func(*v1.Pod) error
}

func (options *WaitForPodOptions) checkPodsRunning() bool {
	if options.CheckPodsRunning == nil {
		return true
	}
	return *options.CheckPodsRunning
}

func (options *WaitForPodOptions) desiredRange() (desiredPodCount, minDesired, maxDesired int, err error) {
	if (options.MinDesiredPodCount != nil) != (options.MaxDesiredPodCount != nil) {
		return 0, 0, 0, fmt.Errorf("both minDesiredPodCount and maxDesiredPodCount must be specified together")
	}
	if options.MinDesiredPodCount != nil && options.MaxDesiredPodCount != nil {
		if options.Toleration != nil {
			return 0, 0, 0, fmt.Errorf("cannot specify both minDesiredPodCount/maxDesiredPodCount and toleration")
		}
		if *options.MinDesiredPodCount > *options.MaxDesiredPodCount {
			return 0, 0, 0, fmt.Errorf("minDesiredPodCount (%d) cannot be greater than maxDesiredPodCount (%d)", *options.MinDesiredPodCount, *options.MaxDesiredPodCount)
		}
		minDesired = *options.MinDesiredPodCount
		maxDesired = *options.MaxDesiredPodCount
		if options.DesiredPodCount != nil {
			desiredPodCount = options.DesiredPodCount()
		} else {
			desiredPodCount = (minDesired + maxDesired) / 2
		}
		return desiredPodCount, minDesired, maxDesired, nil
	}

	if options.DesiredPodCount != nil {
		desiredPodCount = options.DesiredPodCount()
	}

	if options.Toleration != nil {
		toleration := *options.Toleration
		if toleration < 0.0 || toleration > 100.0 {
			return 0, 0, 0, fmt.Errorf("toleration (%v) must be between 0 and 100", toleration)
		}
		margin := int(math.Ceil(float64(desiredPodCount) * toleration / 100.0))
		minDesired = desiredPodCount - margin
		if minDesired < 0 {
			minDesired = 0
		}
		maxDesired = desiredPodCount + margin
		return desiredPodCount, minDesired, maxDesired, nil
	}

	return desiredPodCount, desiredPodCount, desiredPodCount, nil
}

// ParseDesiredPodRange parses optional minDesiredPodCount, maxDesiredPodCount, and toleration from measurement params.
func ParseDesiredPodRange(params map[string]interface{}) (minDesired, maxDesired *int, toleration *float64, err error) {
	minVal, minErr := util.GetInt(params, "minDesiredPodCount")
	if minErr != nil && !util.IsErrKeyNotFound(minErr) {
		return nil, nil, nil, minErr
	}
	maxVal, maxErr := util.GetInt(params, "maxDesiredPodCount")
	if maxErr != nil && !util.IsErrKeyNotFound(maxErr) {
		return nil, nil, nil, maxErr
	}

	hasMin := minErr == nil
	hasMax := maxErr == nil
	if hasMin != hasMax {
		return nil, nil, nil, fmt.Errorf("both minDesiredPodCount and maxDesiredPodCount must be specified together")
	}

	tolVal, tolErr := util.GetFloat64(params, "toleration")
	if tolErr != nil && !util.IsErrKeyNotFound(tolErr) {
		return nil, nil, nil, tolErr
	}
	hasToleration := tolErr == nil

	if hasMin && hasMax {
		if hasToleration {
			return nil, nil, nil, fmt.Errorf("cannot specify both minDesiredPodCount/maxDesiredPodCount and toleration")
		}
		if minVal > maxVal {
			return nil, nil, nil, fmt.Errorf("minDesiredPodCount (%d) cannot be greater than maxDesiredPodCount (%d)", minVal, maxVal)
		}
		return &minVal, &maxVal, nil, nil
	}

	if hasToleration {
		if tolVal < 0.0 || tolVal > 100.0 {
			return nil, nil, nil, fmt.Errorf("toleration (%v) must be between 0 and 100", tolVal)
		}
		return nil, nil, &tolVal, nil
	}

	return nil, nil, nil, nil
}

// CalculateDesiredPodRange calculates the desired [minDesired, maxDesired] range and margin from params and desiredPodCount.
func CalculateDesiredPodRange(params map[string]interface{}, desiredPodCount int) (minDesired, maxDesired, margin int, err error) {
	minPtr, maxPtr, tolPtr, err := ParseDesiredPodRange(params)
	if err != nil {
		return 0, 0, 0, err
	}
	opts := &WaitForPodOptions{
		DesiredPodCount:    func() int { return desiredPodCount },
		MinDesiredPodCount: minPtr,
		MaxDesiredPodCount: maxPtr,
		Toleration:         tolPtr,
	}
	_, minDesired, maxDesired, err = opts.desiredRange()
	if err != nil {
		return 0, 0, 0, err
	}
	margin = (maxDesired - minDesired) / 2
	return minDesired, maxDesired, margin, nil
}

// PodLister is an interface around listing pods.
type PodLister interface {
	List() ([]*v1.Pod, error)
	String() string
}

// WaitForPods waits till desired number of pods is running (or present if CheckPodsRunning is false).
// The current set of pods are fetched by calling List() on the provided PodStore.
// In the case of failure returns list of pods that were in unexpected state
func WaitForPods(ctx context.Context, ps PodLister, options *WaitForPodOptions) (*PodsStatus, error) {
	if _, _, _, err := options.desiredRange(); err != nil {
		return nil, err
	}

	var timeout time.Duration
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		timeout = time.Until(deadline)
	}
	klog.V(2).Infof("%s: %s: starting with timeout: %v", options.CallerName, ps.String(), timeout)
	oldPods, err := ps.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	scaling := uninitialized
	var oldPodsStatus PodsStartupStatus
	var lastIsPodUpdatedError error

	var tolerationCh <-chan time.Time
	if options.TolerationTimeout > 0 {
		timer := time.NewTimer(options.TolerationTimeout)
		defer timer.Stop()
		tolerationCh = timer.C
	}

	var tolerationExpired bool
	var tolerationExpiredAt time.Time
	checkPodsRunning := options.checkPodsRunning()

	for {
		select {
		case <-ctx.Done():
			if latestPods, listErr := ps.List(); listErr == nil {
				oldPods = latestPods
			}
			desiredPodCount, minDesired, maxDesired, _ := options.desiredRange()
			oldPodsStatus = ComputePodsStartupStatus(oldPods, desiredPodCount, options.IsPodUpdated)
			pods := ComputePodsStatus(oldPods)
			if ctx.Err() == context.DeadlineExceeded {
				klog.V(2).Infof("%s: %s: expected %d-%d pods, got %d pods (not RunningAndReady pods: %v)", options.CallerName, ps.String(), minDesired, maxDesired, len(oldPods), pods.NotRunningAndReady())
				klog.V(2).Infof("%s: %s: all pods: %v", options.CallerName, ps.String(), pods)
				klog.V(2).Infof("%s: %s: last IsPodUpdated error: %v", options.CallerName, ps.String(), lastIsPodUpdatedError)
				// In case of scaling down we expect unhealth pods to be in TERMINATING state
				// If we end up with more than expected pods and they are all in RunningAndReady state
				// we won't report them to the user
				if minDesired == maxDesired {
					return pods.NotRunningAndReady(), fmt.Errorf("got %w while waiting for %d pods to be running in %s - summary of pods : %s", ctx.Err(),
						minDesired, ps.String(), oldPodsStatus.String())
				}
				return pods.NotRunningAndReady(), fmt.Errorf("got %w while waiting for %d-%d pods to be running in %s - summary of pods : %s", ctx.Err(),
					minDesired, maxDesired, ps.String(), oldPodsStatus.String())
			}
			return pods.NotRunningAndReady(), ctx.Err()

		case <-tolerationCh:
			desiredPodCount, minDesired, maxDesired, err := options.desiredRange()
			if err != nil {
				return nil, err
			}
			pods, err := ps.List()
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}
			podsStatus := ComputePodsStartupStatus(pods, desiredPodCount, options.IsPodUpdated)
			if podsStatus.LastIsPodUpdatedError != nil {
				lastIsPodUpdatedError = podsStatus.LastIsPodUpdatedError
			}
			klog.V(2).Infof("%s: %s: toleration timeout expired, pods status: %s", options.CallerName, ps.String(), podsStatus.String())
			if isPodsStatusAcceptable(pods, podsStatus, desiredPodCount, minDesired, maxDesired, options.CountErrorMargin, checkPodsRunning) {
				return nil, nil
			}
			tolerationExpired = true
			tolerationExpiredAt = time.Now()
			oldPods = pods
			oldPodsStatus = podsStatus

		case <-time.After(options.WaitForPodsInterval):
			desiredPodCount, minDesired, maxDesired, err := options.desiredRange()
			if err != nil {
				return nil, err
			}

			switch {
			case len(oldPods) < minDesired:
				scaling = up
			case len(oldPods) > maxDesired:
				scaling = down
			default:
				scaling = none
			}

			pods, err := ps.List()
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}
			podsStatus := ComputePodsStartupStatus(pods, desiredPodCount, options.IsPodUpdated)
			if podsStatus.LastIsPodUpdatedError != nil {
				lastIsPodUpdatedError = podsStatus.LastIsPodUpdatedError
			}

			diff := DiffPods(oldPods, pods)
			deletedPods := diff.DeletedPods()
			if scaling == up && len(deletedPods) > 0 {
				klog.Warningf("%s: %s: %d pods disappeared: %v", options.CallerName, ps.String(), len(deletedPods), strings.Join(deletedPods, ", "))
			}
			addedPods := diff.AddedPods()
			if scaling == down && len(addedPods) > 0 {
				klog.Warningf("%s: %s: %d pods appeared: %v", options.CallerName, ps.String(), len(addedPods), strings.Join(addedPods, ", "))
			}
			if podsStatus.String() != oldPodsStatus.String() {
				klog.V(2).Infof("%s: %s: %s", options.CallerName, ps.String(), podsStatus.String())
			}
			if isPodsStatusAcceptable(pods, podsStatus, desiredPodCount, minDesired, maxDesired, options.CountErrorMargin, checkPodsRunning) {
				if tolerationExpired {
					delay := time.Since(tolerationExpiredAt)
					if minDesired == maxDesired {
						return nil, fmt.Errorf("desired number of %d pods in %s reached after tolerationTimeout (%v), delay after tolerationTimeout was %v",
							minDesired, ps.String(), options.TolerationTimeout, delay)
					}
					return nil, fmt.Errorf("desired number of %d-%d pods in %s reached after tolerationTimeout (%v), delay after tolerationTimeout was %v",
						minDesired, maxDesired, ps.String(), options.TolerationTimeout, delay)
				}
				return nil, nil
			}
			oldPods = pods
			oldPodsStatus = podsStatus
		}
	}
}

func isPodsStatusAcceptable(pods []*v1.Pod, podsStatus PodsStartupStatus, desiredPodCount, minDesired, maxDesired, countErrorMargin int, checkPodsRunning bool) bool {
	if !checkPodsRunning {
		return len(pods) >= minDesired && len(pods) <= maxDesired
	}
	// We allow inactive pods (e.g. eviction happened).
	// We wait until there is a desired number of pods running and all other pods are inactive.
	if len(pods) == (podsStatus.Running+podsStatus.Inactive) &&
		podsStatus.Running == podsStatus.RunningUpdated &&
		podsStatus.RunningUpdated >= minDesired && podsStatus.RunningUpdated <= maxDesired {
		return true
	}
	// When using preemptibles on large scale, number of ready nodes is not stable and reaching DesiredPodCount could take a very long time.
	// Overall number of pods (especially Inactive pods) should not grow unchecked.
	if countErrorMargin > 0 && podsStatus.RunningUpdated >= desiredPodCount-countErrorMargin && len(pods)-podsStatus.Inactive <= desiredPodCount && podsStatus.Inactive <= countErrorMargin {
		return true
	}
	return false
}
