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

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	memoryProfileName = "MemoryProfile"
	cpuProfileName    = "CPUProfile"
)

func init() {
	measurement.Register(memoryProfileName, createMemoryProfileMeasurement)
	measurement.Register(cpuProfileName, createCPUProfileMeasurement)
}

func createMemoryProfileMeasurement() measurement.Measurement {
	return &memoryProfileMeasurement{}
}

type memoryProfileMeasurement struct{}

// Execute gathers memory profile of a given component.
func (c *memoryProfileMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	// TODO: Introduce two actions:
	// - start - start periodically gathering memory profiles
	// - gather - finish gathering profile and gather memory allocations profile
	return createMeasurement(c, config, "heap")
}

// Dispose cleans up after the measurement.
func (*memoryProfileMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*memoryProfileMeasurement) String() string {
	return memoryProfileName
}

func createCPUProfileMeasurement() measurement.Measurement {
	return &cpuProfileMeasurement{}
}

type cpuProfileMeasurement struct{}

// Execute gathers cpu profile of a given component.
func (c *cpuProfileMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	// TODO: Introduce two actions:
	// - start - start periodically gathering cpu profiles
	// - gather - finish gathering profile
	return createMeasurement(c, config, "profile")
}

// Dispose cleans up after the measurement.
func (*cpuProfileMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*cpuProfileMeasurement) String() string {
	return cpuProfileName
}

func createMeasurement(caller measurement.Measurement, config *measurement.MeasurementConfig, profileKind string) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	componentName, err := util.GetString(config.Params, "componentName")
	if err != nil {
		return summaries, err
	}
	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return summaries, err
	}
	host, err := util.GetStringOrDefault(config.Params, "host", config.ClusterFramework.GetClusterConfig().MasterIP)
	if err != nil {
		return summaries, err
	}

	return gatherProfile(caller, componentName, profileKind, host, provider)
}

func gatherProfile(caller measurement.Measurement, componentName, profileKind, host, provider string) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	profilePort, err := getPortForComponent(componentName)
	if err != nil {
		return summaries, fmt.Errorf("profile gathering failed finding component port: %v", err)
	}

	// Get the profile data over SSH.
	getCommand := fmt.Sprintf("curl -s localhost:%v/debug/pprof/%s", profilePort, profileKind)
	sshResult, err := measurementutil.SSH(getCommand, host+":22", provider)
	if err != nil {
		if provider == "gke" {
			// Only logging error for gke. SSHing to gke master is not supported.
			klog.Errorf("%s: failed to execute curl command on master through SSH: %v", caller, err)
			return summaries, nil
		}
		return summaries, fmt.Errorf("failed to execute curl command on master through SSH: %v", err)
	}

	profilePrefix := componentName
	switch {
	case profileKind == "heap":
		profilePrefix += "_MemoryProfile"
	case strings.HasPrefix(profileKind, "profile"):
		profilePrefix += "_CPUProfile"
	// TODO: Add memory allocations profile.
	default:
		return summaries, fmt.Errorf("unknown profile kind provided: %s", profileKind)
	}

	rawprofile := &profileSummary{
		name:    profilePrefix,
		content: sshResult.Stdout,
	}
	summaries = append(summaries, rawprofile)
	return summaries, nil
}

func getPortForComponent(componentName string) (int, error) {
	switch componentName {
	case "kube-apiserver":
		return 8080, nil
	case "kube-scheduler":
		return 10251, nil
	case "kube-controller-manager":
		return 10252, nil
	}
	return -1, fmt.Errorf("port for component %v unknown", componentName)
}

type profileSummary struct {
	name    string
	content string
}

// SummaryName returns name of the summary.
func (p *profileSummary) SummaryName() string {
	return p.name
}

// PrintSummary returns summary as a string.
func (p *profileSummary) PrintSummary() (string, error) {
	return p.content, nil
}
