/*
Copyright 2021 The Kubernetes Authors.

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
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	containerRestartsMeasurementName = "ContainerRestarts"

	containerRestartCountQuery = `changes(container_start_time_seconds[%v])`
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&containerRestartsGatherer{})
	}
	if err := measurement.Register(containerRestartsMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", containerRestartsMeasurementName, err)
	}
}

type containerRestartsGatherer struct{}

type ContainerInfo struct {
	Container string `yaml:"container"`
	Pod       string `yaml:"pod"`
	Namespace string `yaml:"namespace"`
}

type ContainerRestartsInfo struct {
	ContainerInfo `yaml:",inline"`
	RestartCount  int `yaml:"restartCount"`
}

type restartCountOverride struct {
	ContainerInfo   `yaml:",inline"`
	AllowedRestarts int `yaml:"allowedRestarts"`
	podNameRegex    *regexp.Regexp
}

func (a *containerRestartsGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	restartCountOverrides, err := a.getOverrides(config)
	if err != nil {
		return nil, err
	}

	defaultAllowedRestarts, err := util.GetIntOrDefault(config.Params, "defaultAllowedRestarts", 0)
	if err != nil {
		return nil, err
	}

	containerRestarts, err := a.gatherContainerRestarts(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(containerRestarts)
	if err != nil {
		return nil, err
	}

	summaries := []measurement.Summary{measurement.CreateSummary(containerRestartsMeasurementName, "json", content)}
	if badContainers := a.validateRestarts(containerRestarts, defaultAllowedRestarts, restartCountOverrides); len(badContainers) > 0 {
		return summaries, errors.NewMetricViolationError("container restarts", fmt.Sprintf("container restart count validation: %v", badContainers))
	}
	return summaries, nil
}

func (a *containerRestartsGatherer) getOverrides(config *measurement.Config) ([]*restartCountOverride, error) {
	restartCountOverridesString, err := util.GetStringOrDefault(config.Params, "customAllowedRestarts", "")
	if err != nil {
		return nil, err
	}

	var restartCountOverrides []*restartCountOverride
	if err := yaml.Unmarshal([]byte(restartCountOverridesString), &restartCountOverrides); err != nil {
		return nil, err
	}

	for _, car := range restartCountOverrides {
		podNamePattern := strings.ReplaceAll(strings.ReplaceAll(car.Pod, ".", "\\."), "*", ".*")
		car.podNameRegex = regexp.MustCompile("^" + podNamePattern + "$")
	}
	return restartCountOverrides, nil
}

func (a *containerRestartsGatherer) gatherContainerRestarts(executor QueryExecutor, startTime, endTime time.Time) ([]ContainerRestartsInfo, error) {
	measurementDuration := endTime.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)
	query := fmt.Sprintf(containerRestartCountQuery, promDuration)
	samples, err := executor.Query(query, endTime)
	if err != nil {
		return nil, err
	}

	extractCommon := func(sample *model.Sample) (string, string, string) {
		return string(sample.Metric["container"]), string(sample.Metric["pod"]), string(sample.Metric["namespace"])
	}

	result := []ContainerRestartsInfo{}
	for _, sample := range samples {
		container, pod, namespace := extractCommon(sample)
		count := int(math.Round(float64(sample.Value)))
		cri := ContainerRestartsInfo{
			ContainerInfo: ContainerInfo{
				Container: container,
				Pod:       pod,
				Namespace: namespace,
			},
			RestartCount: count,
		}
		result = append(result, cri)
	}
	return result, nil
}

func (a *containerRestartsGatherer) validateRestarts(restartsInfos []ContainerRestartsInfo, defaultAllowedRestarts int, restartCountOverrides []*restartCountOverride) []error {
	badContainers := make([]error, 0)
	for _, ri := range restartsInfos {
		allowedRestarts := defaultAllowedRestarts
		for _, override := range restartCountOverrides {
			if override.podNameRegex.MatchString(ri.Pod) && allowedRestarts < override.AllowedRestarts {
				allowedRestarts = override.AllowedRestarts
			}
		}
		if ri.RestartCount > allowedRestarts {
			badContainers = append(badContainers, fmt.Errorf("restartCount(%+v) = %v, expected <= %v", ri.ContainerInfo, ri.RestartCount, allowedRestarts))
		}
	}
	return badContainers
}

func (a *containerRestartsGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (a *containerRestartsGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (*containerRestartsGatherer) String() string {
	return containerRestartsMeasurementName
}
