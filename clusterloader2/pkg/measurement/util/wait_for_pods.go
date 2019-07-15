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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
	Namespace           string
	LabelSelector       string
	FieldSelector       string
	DesiredPodCount     int
	EnableLogging       bool
	CallerName          string
	WaitForPodsInterval time.Duration
}

// WaitForPods waits till disire nuber of pods is running.
// Pods are be specified by namespace, field and/or label selectors.
// If stopCh is closed before all pods are running, the error will be returned.
func WaitForPods(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForPodOptions) error {
	// TODO(#269): Change to shared podStore.
	ps, err := NewPodStore(clientSet, options.Namespace, options.LabelSelector, options.FieldSelector)
	if err != nil {
		return fmt.Errorf("pod store creation error: %v", err)
	}
	defer ps.Stop()

	var podsStatus PodsStartupStatus
	selectorsString := CreateSelectorsString(options.Namespace, options.LabelSelector, options.FieldSelector)
	scaling := uninitialized
	var oldPods []*corev1.Pod
	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for %d pods to be running in namespace '%v' with labels '%v' and fields '%v' - only %d found running",
				options.DesiredPodCount, options.Namespace, options.LabelSelector, options.FieldSelector, podsStatus.Running)
		case <-time.After(options.WaitForPodsInterval):
			pods := ps.List()
			podsStatus = ComputePodsStartupStatus(pods, options.DesiredPodCount)
			if scaling != uninitialized {
				diff := DiffPods(oldPods, pods)
				deletedPods := diff.DeletedPods()
				if scaling != down && len(deletedPods) > 0 {
					klog.Errorf("%s: %s: %d pods disappeared: %v", options.CallerName, selectorsString, len(deletedPods), strings.Join(deletedPods, ", "))
					klog.Infof("%s: %v", options.CallerName, diff.String(sets.NewString()))
				}
				addedPods := diff.AddedPods()
				if scaling != up && len(addedPods) > 0 {
					klog.Errorf("%s: %s: %d pods appeared: %v", options.CallerName, selectorsString, len(deletedPods), strings.Join(deletedPods, ", "))
					klog.Infof("%s: %v", options.CallerName, diff.String(sets.NewString()))
				}
			} else {
				switch {
				case len(pods) == options.DesiredPodCount:
					scaling = none
				case len(pods) < options.DesiredPodCount:
					scaling = up
				case len(pods) > options.DesiredPodCount:
					scaling = down
				}
			}
			if options.EnableLogging {
				klog.Infof("%s: %s: %s", options.CallerName, selectorsString, podsStatus.String())
			}
			// We allow inactive pods (e.g. eviction happened).
			// We wait until there is a desired number of pods running and all other pods are inactive.
			if len(pods) == (podsStatus.Running+podsStatus.Inactive) && podsStatus.Running == options.DesiredPodCount {
				return nil
			}
			oldPods = pods
		}
	}
}
