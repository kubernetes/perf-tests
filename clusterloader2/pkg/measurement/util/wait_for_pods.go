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
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	uninitialized = iota
	up
	down
	none
)

// WaitForPodOptions is an options used by WaitForPods methods.
type WaitForPodOptions struct {
	Selector            *ObjectSelector
	DesiredPodCount     func() int
	CountErrorMargin    int
	CallerName          string
	WaitForPodsInterval time.Duration

	// IsPodUpdated can be used to detect which pods have been already updated.
	// nil value means all pods are updated.
	IsPodUpdated func(*v1.Pod) bool
}

// WaitForPods waits till desired number of pods is running.
// Pods are be specified by namespace, field and/or label selectors.
// If stopCh is closed before all pods are running, the error will be returned.
func WaitForPods(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForPodOptions) error {
	ps, err := NewPodStore(clientSet, options.Selector)
	if err != nil {
		return fmt.Errorf("pod store creation error: %v", err)
	}
	defer ps.Stop()

	oldPods := ps.List()
	scaling := uninitialized
	var podsStatus PodsStartupStatus

	for {
		select {
		case <-stopCh:
			desiredPodCount := options.DesiredPodCount()
			klog.V(2).Infof("%s: %s: pods status: %v", options.CallerName, options.Selector.String(), ComputePodsStatus(oldPods, desiredPodCount))
			return fmt.Errorf("timeout while waiting for %d pods to be running in namespace '%v' with labels '%v' and fields '%v' - summary of pods : %s",
				desiredPodCount, options.Selector.Namespace, options.Selector.LabelSelector, options.Selector.FieldSelector, podsStatus.String())
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

			pods := ps.List()
			podsStatus = ComputePodsStartupStatus(pods, desiredPodCount, options.IsPodUpdated)

			diff := DiffPods(oldPods, pods)
			deletedPods := diff.DeletedPods()
			if scaling != down && len(deletedPods) > 0 {
				klog.Errorf("%s: %s: %d pods disappeared: %v", options.CallerName, options.Selector.String(), len(deletedPods), strings.Join(deletedPods, ", "))
			}
			addedPods := diff.AddedPods()
			if scaling != up && len(addedPods) > 0 {
				klog.Errorf("%s: %s: %d pods appeared: %v", options.CallerName, options.Selector.String(), len(addedPods), strings.Join(addedPods, ", "))
			}
			klog.V(2).Infof("%s: %s: %s", options.CallerName, options.Selector.String(), podsStatus.String())
			// We allow inactive pods (e.g. eviction happened).
			// We wait until there is a desired number of pods running and all other pods are inactive.
			if len(pods) == (podsStatus.Running+podsStatus.Inactive) && podsStatus.Running == podsStatus.RunningUpdated && podsStatus.RunningUpdated == desiredPodCount {
				return nil
			}
			// When using preemptibles on large scale, number of ready nodes is not stable and reaching DesiredPodCount could take a very long time.
			// Overall number of pods (especially Inactive pods) should not grow unchecked.
			if options.CountErrorMargin > 0 && podsStatus.RunningUpdated >= desiredPodCount-options.CountErrorMargin && len(pods)-podsStatus.Inactive <= desiredPodCount && podsStatus.Inactive <= options.CountErrorMargin {
				return nil
			}
			oldPods = pods
		}
	}
}
