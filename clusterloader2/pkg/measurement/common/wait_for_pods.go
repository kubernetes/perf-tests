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

package common

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForPodsTimeout  = 60 * time.Second
	defaultWaitForPodsInterval = 5 * time.Second
)

func init() {
	measurement.Register("WaitForRunningPods", createWaitForRunningPodsMeasurement)
}

func createWaitForRunningPodsMeasurement() measurement.Measurement {
	return &waitForRunningPodsMeasurement{}
}

type waitForRunningPodsMeasurement struct{}

// Execute waits until desired number of pods are running or until timeout happens.
// Pods can be specified by namespace and/or label selectors.
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
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForPodsTimeout)
	if err != nil {
		return summaries, err
	}

	stopCh := make(chan struct{})
	time.AfterFunc(timeout, func() {
		close(stopCh)
	})
	ps, err := measurementutil.NewPodStore(config.ClientSet, namespace, labelSelector)
	if err != nil {
		return summaries, fmt.Errorf("pod store creation error: %v", err)
	}
	defer ps.Stop()
	return summaries, waitForPods(ps, namespace, labelSelector, desiredPodCount, stopCh, true)
}

func waitForPods(ps *measurementutil.PodStore, namespace, labelSelector string, desiredPodCount int, stopCh <-chan struct{}, log bool) error {
	var runningPodsCount int
	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for %d pods to be running in namespace '%v' with labels '%v' - only %d found running", desiredPodCount, namespace, labelSelector, runningPodsCount)
		case <-time.After(defaultWaitForPodsInterval):
			pods, err := ps.FilteredList(namespace, labelSelector)
			if err != nil {
				return fmt.Errorf("filtering list error: %v", err)
			}
			runningPodsCount = 0
			for i := range pods {
				if pods[i].Status.Phase == corev1.PodRunning {
					runningPodsCount++
				}
			}
			if log {
				glog.Infof("WaitForRunningPods: namespace(%s), labelSelector(%s): running %d / %d", namespace, labelSelector, runningPodsCount, desiredPodCount)
			}
			if runningPodsCount == desiredPodCount {
				return nil
			}
		}
	}
}
