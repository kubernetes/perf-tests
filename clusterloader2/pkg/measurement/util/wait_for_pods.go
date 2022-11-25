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
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
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
	CountErrorMargin    int
	CallerName          string
	WaitForPodsInterval time.Duration

	// IsPodUpdated can be used to detect which pods have been already updated.
	// nil value means all pods are updated.
	IsPodUpdated func(*v1.Pod) error
}

// PodLister is an interface around listing pods.
type PodLister interface {
	List() ([]*v1.Pod, error)
	String() string
}

// WaitForPods waits till desired number of pods is running.
// The current set of pods are fetched by calling List() on the provided PodStore.
// In the case of failure returns list of pods that were in unexpected state
func WaitForPods(ctx context.Context, ps PodLister, options *WaitForPodOptions) (*PodsStatus, error) {
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

	for {
		select {
		case <-ctx.Done():
			desiredPodCount := options.DesiredPodCount()
			pods := ComputePodsStatus(oldPods)
			klog.V(2).Infof("%s: %s: expected %d pods, got %d pods (not RunningAndReady pods: %v)", options.CallerName, ps.String(), desiredPodCount, len(oldPods), pods.NotRunningAndReady())
			klog.V(2).Infof("%s: %s: all pods: %v", options.CallerName, ps.String(), pods)
			klog.V(2).Infof("%s: %s: last IsPodUpdated error: %v", options.CallerName, ps.String(), lastIsPodUpdatedError)
			// In case of scaling down we expect unhealth pods to be in TERMINATING state
			// If we end up with more than expected pods and they are all in RunningAndReady state
			// we won't report them to the user
			return pods.NotRunningAndReady(), fmt.Errorf("timeout while waiting for %d pods to be running in %s - summary of pods : %s",
				desiredPodCount, ps.String(), oldPodsStatus.String())
		case <-time.After(options.WaitForPodsInterval):
			desiredPodCount := options.DesiredPodCount()

			switch {
			case len(oldPods) == desiredPodCount:
				scaling = none
			case len(oldPods) < desiredPodCount:
				scaling = up
			case len(oldPods) > desiredPodCount:
				scaling = down
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
			// We allow inactive pods (e.g. eviction happened).
			// We wait until there is a desired number of pods running and all other pods are inactive.
			if len(pods) == (podsStatus.Running+podsStatus.Inactive) && podsStatus.Running == podsStatus.RunningUpdated && podsStatus.RunningUpdated == desiredPodCount {
				return nil, nil
			}
			// When using preemptibles on large scale, number of ready nodes is not stable and reaching DesiredPodCount could take a very long time.
			// Overall number of pods (especially Inactive pods) should not grow unchecked.
			if options.CountErrorMargin > 0 && podsStatus.RunningUpdated >= desiredPodCount-options.CountErrorMargin && len(pods)-podsStatus.Inactive <= desiredPodCount && podsStatus.Inactive <= options.CountErrorMargin {
				return nil, nil
			}
			oldPods = pods
			oldPodsStatus = podsStatus
		}
	}
}
