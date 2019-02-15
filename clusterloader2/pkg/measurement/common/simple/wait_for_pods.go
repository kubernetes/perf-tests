/*
Copyright 2018 The Kubernetes Authors.

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

package simple

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForPodsTimeout         = 60 * time.Second
	defaultWaitForPodsInterval        = 5 * time.Second
	waitForRunningPodsMeasurementName = "WaitForRunningPods"
)

func init() {
	measurement.Register(waitForRunningPodsMeasurementName, createWaitForRunningPodsMeasurement)
}

func createWaitForRunningPodsMeasurement() measurement.Measurement {
	return &waitForRunningPodsMeasurement{}
}

type waitForRunningPodsMeasurement struct{}

// Execute waits until desired number of pods are running or until timeout happens.
// Pods can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
func (*waitForRunningPodsMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	desiredPodCount, err := util.GetInt(config.Params, "desiredPodCount")
	if err != nil {
		return summaries, err
	}
	namespace, err := util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
	if err != nil {
		return summaries, err
	}
	labelSelector, err := util.GetStringOrDefault(config.Params, "labelSelector", "")
	if err != nil {
		return summaries, err
	}
	fieldSelector, err := util.GetStringOrDefault(config.Params, "fieldSelector", "")
	if err != nil {
		return summaries, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPodsTimeout)
	if err != nil {
		return summaries, err
	}

	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})
	return summaries, waitForPods(config.ClientSets.GetClient(), namespace, labelSelector, fieldSelector, desiredPodCount, stopCh, true, waitForRunningPodsMeasurementName)
}

// Dispose cleans up after the measurement.
func (*waitForRunningPodsMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForRunningPodsMeasurement) String() string {
	return waitForRunningPodsMeasurementName
}

const (
	uninitialized = iota
	up
	down
	none
)

func waitForPods(clientSet clientset.Interface, namespace, labelSelector, fieldSelector string, desiredPodCount int, stopCh <-chan struct{}, log bool, callerName string) error {
	// TODO(#269): Change to shared podStore.
	ps, err := measurementutil.NewPodStore(clientSet, namespace, labelSelector, fieldSelector)
	if err != nil {
		return fmt.Errorf("pod store creation error: %v", err)
	}
	defer ps.Stop()

	var podsStatus measurementutil.PodsStartupStatus
	selectorsString := measurementutil.CreateSelectorsString(namespace, labelSelector, fieldSelector)
	scaling := uninitialized
	var oldPods []*corev1.Pod
	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for %d pods to be running in namespace '%v' with labels '%v' and fields '%v' - only %d found running", desiredPodCount, namespace, labelSelector, fieldSelector, podsStatus.Running)
		case <-time.After(defaultWaitForPodsInterval):
			pods := ps.List()
			podsStatus = measurementutil.ComputePodsStartupStatus(pods, desiredPodCount)
			if scaling != uninitialized {
				diff := measurementutil.DiffPods(oldPods, pods)
				deletedPods := diff.DeletedPods()
				if scaling != down && len(deletedPods) > 0 {
					klog.Errorf("%s: %s: %d pods disappeared: %v", callerName, selectorsString, len(deletedPods), strings.Join(deletedPods, ", "))
					klog.Infof("%s: %v", callerName, diff.String(sets.NewString()))
				}
				addedPods := diff.AddedPods()
				if scaling != up && len(addedPods) > 0 {
					klog.Errorf("%s: %s: %d pods appeared: %v", callerName, selectorsString, len(deletedPods), strings.Join(deletedPods, ", "))
					klog.Infof("%s: %v", callerName, diff.String(sets.NewString()))
				}
			} else {
				switch {
				case len(pods) == desiredPodCount:
					scaling = none
				case len(pods) < desiredPodCount:
					scaling = up
				case len(pods) > desiredPodCount:
					scaling = down
				}
			}
			if log {
				klog.Infof("%s: %s: %s", callerName, selectorsString, podsStatus.String())
			}
			// We allow inactive pods (e.g. eviction happened).
			// We wait until there is a desired number of pods running and all other pods are inactive.
			if len(pods) == (podsStatus.Running+podsStatus.Inactive) && podsStatus.Running == desiredPodCount {
				return nil
			}
			oldPods = pods
		}
	}
}
