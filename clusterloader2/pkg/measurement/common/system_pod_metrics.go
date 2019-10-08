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

// Gathers metrics for system pods, right now it only gathers container restart counts
type systemPodMetricsMeasurement struct{}

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

	klog.Info("collecting system pod metrics...")
	lst, err := getPodList(config.ClusterFramework.GetClientSets().GetClient())
	if err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(extractMetrics(lst))
	if err != nil {
		return nil, err
	}

	summary := measurement.CreateSummary(systemPodMetricsName, "json", content)
	return []measurement.Summary{summary}, nil
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

func extractMetrics(lst *v1.PodList) systemPodsMetrics {
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
	return metrics
}

// Dispose cleans up after the measurement.
func (m *systemPodMetricsMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*systemPodMetricsMeasurement) String() string {
	return systemPodMetricsName
}
