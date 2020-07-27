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
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	systemPodMetricsName              = "SystemPodMetrics"
	systemNamespace                   = "kube-system"
	systemPodMetricsEnabledFlagName   = "systemPodMetricsEnabled"
	restartThresholdOverridesFlagName = "restartCountThresholdOverrides"
	enableRestartCountCheckFlagName   = "enableRestartCountCheck"
	defaultRestartCountThresholdKey   = "default"
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
	Name              string `json:"name"`
	RestartCount      int32  `json:"restartCount"`
	LastRestartReason string `json:"lastRestartReason"`
}

type podMetrics struct {
	Name       string             `json:"name"`
	Containers []containerMetrics `json:"containers"`
}

type systemPodsMetrics struct {
	Pods []podMetrics `json:"pods"`
}

// Execute gathers and prints system pod metrics.
func (m *systemPodMetricsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	systemPodMetricsEnabled, err := util.GetBoolOrDefault(config.Params, systemPodMetricsEnabledFlagName, false)
	if err != nil {
		return nil, err
	}
	if !systemPodMetricsEnabled {
		klog.V(2).Info("skipping collection of system pod metrics")
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

	overrides, err := getThresholdOverrides(config)
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
		if err = validateRestartCounts(metrics, config, overrides); err != nil {
			return summary, err
		}
		return summary, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func getPodMetrics(config *measurement.Config) (*systemPodsMetrics, error) {
	klog.V(2).Info("collecting system pod metrics...")
	lst, err := getPodList(config.ClusterFramework.GetClientSets().GetClient())
	if err != nil {
		return &systemPodsMetrics{}, err
	}
	return extractMetrics(lst), nil
}

func getPodList(client kubernetes.Interface) (*v1.PodList, error) {
	lst, err := client.CoreV1().Pods(systemNamespace).List(context.TODO(), metav1.ListOptions{
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

func validateRestartCounts(metrics *systemPodsMetrics, config *measurement.Config, overrides map[string]int) error {
	enabled, err := util.GetBoolOrDefault(config.Params, enableRestartCountCheckFlagName, false)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	violations := make([]string, 0)
	for _, p := range metrics.Pods {
		for _, c := range p.Containers {
			maxAllowedRestarts := getMaxAllowedRestarts(c.Name, overrides)
			if c.RestartCount > int32(maxAllowedRestarts) {
				violation := fmt.Sprintf("RestartCount(%v, %v)=%v, want <= %v",
					p.Name, c.Name, c.RestartCount, maxAllowedRestarts)
				violations = append(violations, violation)
			}
		}
	}

	if len(violations) == 0 {
		return nil
	}
	violationsJoined := strings.Join(violations, "; ")
	return fmt.Errorf("restart counts violation: %v", violationsJoined)
}

func getMaxAllowedRestarts(containerName string, thresholdOverrides map[string]int) int {
	if override, ok := thresholdOverrides[containerName]; ok {
		return override
	}
	// This allows setting default threshold, which will be used for containers
	// not present in the thresholdOverrides map.
	if override, ok := thresholdOverrides[defaultRestartCountThresholdKey]; ok {
		return override
	}
	return 0 // do not allow any restarts if no override and no default specified
}

/*
getThresholdOverrides deserializes restart count override flag value. The value of
this flag is a map[string]int serialized using yaml format. Note that YamlQuote is used to ensure
proper indentation after gotemplate execution.

Alternatively, we could use yaml map as flag value, but then go templates executor would serialize it
using golang map format (for example "map[c1:4 c2:8]"), but it would require implementation of a parser
for such format. It would also introduce a dependency on golang map serialization format, which might break
clusterloader if format ever changes.
*/
func getThresholdOverrides(config *measurement.Config) (map[string]int, error) {
	serialized, err := util.GetStringOrDefault(config.Params, restartThresholdOverridesFlagName, "")
	if err != nil {
		return make(map[string]int), nil
	}
	var parsed map[string]int
	err = yaml.Unmarshal([]byte(serialized), &parsed)
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("Loaded restart count threshold overrides: %v", parsed)
	return parsed, nil
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
			metrics := containerMetrics{
				Name:         container.Name,
				RestartCount: container.RestartCount,
			}
			if container.LastTerminationState.Terminated != nil {
				metrics.LastRestartReason = container.LastTerminationState.Terminated.String()
			}
			podMetrics.Containers = append(podMetrics.Containers, metrics)
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
