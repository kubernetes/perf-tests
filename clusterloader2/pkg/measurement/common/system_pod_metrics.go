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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	systemPodMetricsName            = "SystemPodMetrics"
	systemNamespace                 = "kube-system"
	systemPodMetricsEnabledFlagName = "systemPodMetricsEnabled"
)

func init() {
	if err := measurement.Register(systemPodMetricsName, createSystemPodMetricsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", systemPodMetricsName, err)
	}
}

func createSystemPodMetricsMeasurement() measurement.Measurement {
	return &systemPodMetricsMeasurement{}
}

// Gathers metrics for system pods, right now it only gathers container restart counts.
// System pods are listed twice: first time for "start" action, second time for "gather" action.
// When executing "gather", initial restart counts are subtracted from the current
// restart counts. In effect, only restarts that happened during test execution
// (between "start" and "gather") are visible in the final summary.
type systemPodMetricsMeasurement struct {
	initSnapshot *systemPodsMetrics
}

type containerMetrics struct {
	Name         string `json:"name"`
	RestartCount int32  `json:"restartCount"`
}

type podMetrics struct {
	Name       string             `json:"name"`
	Containers []containerMetrics `json:"containers"`
}

type systemPodsMetrics struct {
	Pods []podMetrics `json:"pods"`
}

// Execute gathers and prints system pod metrics.
func (m *systemPodMetricsMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	systemPodMetricsEnabled, err := util.GetBoolOrDefault(config.Params, systemPodMetricsEnabledFlagName, false)
	if err != nil {
		return nil, err
	}
	if !systemPodMetricsEnabled {
		klog.Info("skipping collection of system pod metrics")
		return []measurement.Summary{}, nil
	}

	metrics, err := getPodMetrics(config)
	if err != nil {
		return nil, err
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		m.initSnapshot = metrics
		return nil, nil
	case "gather":
		if m.initSnapshot == nil {
			return nil, fmt.Errorf("start needs to be executed before gather")
		}
		subtractInitialRestartCounts(metrics, m.initSnapshot)
		summary, err := buildSummary(metrics)
		if err != nil {
			return nil, err
		}
		return summary, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func getPodMetrics(config *measurement.MeasurementConfig) (*systemPodsMetrics, error) {
	klog.Info("collecting system pod metrics...")
	lst, err := getPodList(config.ClusterFramework.GetClientSets().GetClient())
	if err != nil {
		return &systemPodsMetrics{}, err
	}
	return extractMetrics(lst), nil
}

func getPodList(client kubernetes.Interface) (*v1.PodList, error) {
	lst, err := client.CoreV1().Pods(systemNamespace).List(metav1.ListOptions{
		ResourceVersion: "0", // to read from cache
	})
	if err != nil {
		return nil, err
	}
	return lst, nil
}

func subtractInitialRestartCounts(metrics *systemPodsMetrics, initMetrics *systemPodsMetrics) {
	// podName -> containerName -> restartCount
	initRestarts := make(map[string]map[string]int32)

	for _, initPod := range initMetrics.Pods {
		initRestarts[initPod.Name] = make(map[string]int32)
		for _, initContainer := range initPod.Containers {
			initRestarts[initPod.Name][initContainer.Name] = initContainer.RestartCount
		}
	}

	for _, pod := range metrics.Pods {
		for i, container := range pod.Containers {
			initPod, ok := initRestarts[pod.Name]
			if !ok {
				continue
			}
			initRestartCount, ok := initPod[container.Name]
			if !ok {
				continue
			}
			pod.Containers[i].RestartCount -= initRestartCount
		}
	}
}

func extractMetrics(lst *v1.PodList) *systemPodsMetrics {
	metrics := systemPodsMetrics{
		Pods: []podMetrics{},
	}
	for _, pod := range lst.Items {
		podMetrics := podMetrics{
			Containers: []containerMetrics{},
			Name:       pod.Name,
		}
		for _, container := range pod.Status.ContainerStatuses {
			podMetrics.Containers = append(podMetrics.Containers, containerMetrics{
				Name:         container.Name,
				RestartCount: container.RestartCount,
			})
		}
		metrics.Pods = append(metrics.Pods, podMetrics)
	}
	return &metrics
}

func buildSummary(podMetrics *systemPodsMetrics) ([]measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(podMetrics)
	if err != nil {
		return nil, err
	}

	summary := measurement.CreateSummary(systemPodMetricsName, "json", content)
	return []measurement.Summary{summary}, nil
}

// Dispose cleans up after the measurement.
func (m *systemPodMetricsMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*systemPodMetricsMeasurement) String() string {
	return systemPodMetricsName
}
