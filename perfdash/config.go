/*
Copyright 2016 The Kubernetes Authors.

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

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/klog"
)

// To add new e2e test support, you need to:
//   1) Transform e2e performance test result into *PerfData* in k8s/kubernetes/test/e2e/perftype,
//   and print the PerfData in e2e test log.
//   2) Add corresponding bucket, job and test into *TestConfig*.

// TestDescription contains test name, output file prefix and parser function.
type TestDescription struct {
	Name                  string
	OutputFilePrefix      string
	Parser                func(data []byte, buildNumber int, testResult *BuildData)
	ForceConstantCategory bool
}

// TestDescriptions is a map job->component->description.
type TestDescriptions map[string]map[string][]TestDescription

// Tests is a map from test label to test description.
type Tests struct {
	Prefix       string
	Descriptions TestDescriptions
	BuildsCount  int
	ArtifactsDir string
}

// Jobs is a map from job name to all supported tests in the job.
type Jobs map[string]Tests

const (
	GenericPrometheusQueryMeasurementName = "GenericPrometheusQuery"
)

var (
	// performanceDescriptions contains metrics exported by a --ginko.focus=[Feature:Performance]
	// e2e test
	performanceDescriptions = TestDescriptions{
		"E2E": {
			"DensityResources": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "ResourceUsageSummary",
				Parser:           parseResourceUsageData,
			}},
			"DensityPodStartup": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "PodStartupLatency_PodStartupLatency",
				Parser:           parsePerfData,
			}},
			"DensitySaturationPodStartup": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "PodStartupLatency_SaturationPodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadResources": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "ResourceUsageSummary",
				Parser:           parseResourceUsageData,
			}},
			"LoadCreatePhasePodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "PodStartupLatency_CreatePhasePodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadHighThroughputPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "PodStartupLatency_HighThroughputPodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "PodStartupLatency_PodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadCreatePhaseStatelessPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatelessPodStartupLatency_CreatePhasePodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadHighThroughputStatelessPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatelessPodStartupLatency_HighThroughputPodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadStatelessPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatelessPodStartupLatency_PodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadCreatePhaseStatefulPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatefulPodStartupLatency_CreatePhasePodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadHighThroughputStatefulPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatefulPodStartupLatency_HighThroughputPodStartupLatency",
				Parser:           parsePerfData,
			}},
			"LoadStatefulPodStartup": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "StatefulPodStartupLatency_PodStartupLatency",
				Parser:           parsePerfData,
			}},
			"Resources": []TestDescription{{
				OutputFilePrefix: "ResourceUsageSummary",
				Parser:           parseResourceUsageData,
			}},
		},
		"APIServer": {
			"DensityResponsiveness": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parsePerfData,
			}},
			"DensityRequestCount": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseRequestCountData,
			}},
			"DensityResponsiveness_Prometheus": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parsePerfData,
			}},
			"DensityRequestCount_Prometheus": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parseRequestCountData,
			}},
			"DensityResponsiveness_PrometheusSimple": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parsePerfData,
			}},
			"DensityRequestCount_PrometheusSimple": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parseRequestCountData,
			}},
			"DensityRequestCountByClient": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverRequestCount,
			}},
			"DensityInitEventsCount": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverInitEventsCount,
			}},
			"LoadResponsiveness": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parsePerfData,
			}},
			"LoadRequestCount": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseRequestCountData,
			}},
			"LoadResponsiveness_Prometheus": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parsePerfData,
			}},
			"LoadRequestCount_Prometheus": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parseRequestCountData,
			}},
			"LoadResponsiveness_PrometheusSimple": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parsePerfData,
			}},
			"LoadRequestCount_PrometheusSimple": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parseRequestCountData,
			}},
			"LoadRequestCountByClient": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverRequestCount,
			}},
			"LoadInitEventsCount": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverInitEventsCount,
			}},
			"Responsiveness": []TestDescription{{
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parsePerfData,
			}},
			"RequestCount": []TestDescription{{
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseRequestCountData,
			}},
			"Responsiveness_Prometheus": []TestDescription{{
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parsePerfData,
			}},
			"RequestCount_Prometheus": []TestDescription{{
				OutputFilePrefix: "APIResponsivenessPrometheus",
				Parser:           parseRequestCountData,
			}},
			"Responsiveness_PrometheusSimple": []TestDescription{{
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parsePerfData,
			}},
			"RequestCount_PrometheusSimple": []TestDescription{{
				OutputFilePrefix: "APIResponsivenessPrometheus_simple",
				Parser:           parseRequestCountData,
			}},
			"RequestCountByClient": []TestDescription{{
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverRequestCount,
			}},
			"InitEventsCount": []TestDescription{{
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverInitEventsCount,
			}},
		},
		"Scheduler": {
			"SchedulingLatency": []TestDescription{
				// `density_*` items need to be before the `density` item because of
				// how data file prefixes work. Same applies to SchedulingThroughput.
				{
					Name:             "density_pod-affinity",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("pod-affinity"),
				}, {
					Name:             "density_pod-anti-affinity",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("pod-anti-affinity"),
				}, {
					Name:             "density_pod-topology-spread",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("pod-topology-spread"),
				}, {
					Name:             "density_vanilla",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("vanilla"),
				}, {
					Name:             "density",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("density"),
				}, {
					Name:             "load",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("load"),
				},
			},
			"SchedulingThroughput": []TestDescription{
				{
					Name:             "density_pod-affinity",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("pod-affinity"),
				}, {
					Name:             "density_pod-anti-affinity",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("pod-anti-affinity"),
				}, {
					Name:             "density_pod-topology-spread",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("pod-topology-spread"),
				}, {
					Name:             "density_vanilla",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("vanilla"),
				}, {
					Name:             "density",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("density"),
				}, {
					Name:             "load",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("load"),
				}, {
					Name:             "load",
					OutputFilePrefix: "SchedulingThroughputPrometheus",
					Parser:           parseSchedulingThroughputCL("load-prometheus"),
				},
			},
			"LoadSchedulingLatency": []TestDescription{
				{
					Name:             "load",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingLatency("load"),
				},
			},
			"LoadSchedulingThroughput": []TestDescription{
				{
					Name:             "load",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL("load"),
				},
			},
		},
		"Etcd": {
			"DensityBackendCommitDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("backendCommitDuration"),
			}},
			"DensitySnapshotSaveTotalDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("snapshotSaveTotalDuration"),
			}},
			"DensityPeerRoundTripTime": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("peerRoundTripTime"),
			}},
			"DensityWalFsyncDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("walFsyncDuration"),
			}},
			"LoadBackendCommitDuration": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("backendCommitDuration"),
			}},
			"LoadSnapshotSaveTotalDuration": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("snapshotSaveTotalDuration"),
			}},
			"LoadPeerRoundTripTime": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("peerRoundTripTime"),
			}},
			"LoadWalFsyncDuration": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("walFsyncDuration"),
			}},
		},
		"Network": {
			"Load_NetworkProgrammingLatency": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "NetworkProgrammingLatency",
				Parser:           parsePerfData,
			}},

			"Load_NetworkLatency": []TestDescription{
				{
					// TODO(oxddr): remove this around Sep '19 when we stop showing old data
					Name:             "load",
					OutputFilePrefix: "in_cluster_network_latency",
					Parser:           parsePerfData,
				}, {
					Name:             "load",
					OutputFilePrefix: "InClusterNetworkLatency",
					Parser:           parsePerfData,
				}},

			"Density_NetworkLatency": []TestDescription{
				{
					// TODO(oxddr): remove this around Sep '19 when we stop showing old data
					Name:             "density",
					OutputFilePrefix: "in_cluster_network_latency",
					Parser:           parsePerfData,
				}, {
					Name:             "density",
					OutputFilePrefix: "InClusterNetworkLatency",
					Parser:           parsePerfData,
				}},
		},
		"Network_Baseline": {
			"TCP_P2P": []TestDescription{{
				Name:             "network_performance_tcp-1:1",
				OutputFilePrefix: "NetworkPerformanceMetrics_1:1_TCP_P2P",
				Parser:           parsePerfData,
			}},
			"UDP_P2P": []TestDescription{{
				Name:             "network_performance_udp-1:1",
				OutputFilePrefix: "NetworkPerformanceMetrics_1:1_UDP_P2P",
				Parser:           parsePerfData,
			}},
			"HTTP_P2P": []TestDescription{{
				Name:             "network_performance_http-1:1",
				OutputFilePrefix: "NetworkPerformanceMetrics_1:1_HTTP_P2P",
				Parser:           parsePerfData,
			}},
		},
		"Network_Scale": {
			"TCP_P2P_N:M": []TestDescription{{
				Name:             "network_performance_tcp-50:50",
				OutputFilePrefix: "NetworkPerformanceMetrics_N:M_TCP_P2P",
				Parser:           parsePerfData,
			}},
			"UDP_P2P_N:M": []TestDescription{{
				Name:             "network_performance_udp-50:50",
				OutputFilePrefix: "NetworkPerformanceMetrics_N:M_UDP_P2P",
				Parser:           parsePerfData,
			}},
			"HTTP_P2P_N:M": []TestDescription{{
				Name:             "network_performance_http-50:50",
				OutputFilePrefix: "NetworkPerformanceMetrics_N:M_HTTP_P2P",
				Parser:           parsePerfData,
			}},
		},
		"DNS": {
			"Load_DNSLookupLatency": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "DnsLookupLatency",
				Parser:           parsePerfData,
			}},
			"Density_DNSLookupLatency": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "DnsLookupLatency",
				Parser:           parsePerfData,
			}},
			"DNSLookupLatency": []TestDescription{{
				OutputFilePrefix: "DnsLookupLatency",
				Parser:           parsePerfData,
			}},
		},
		"SystemPodMetrics": {
			"Load_SystemPodMetrics": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "SystemPodMetrics",
				Parser:           parseSystemPodMetrics,
			}},
			"Density_SystemPodMetrics": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "SystemPodMetrics",
				Parser:           parseSystemPodMetrics,
			}},
			"SystemPodMetrics": []TestDescription{{
				OutputFilePrefix: "SystemPodMetrics",
				Parser:           parseSystemPodMetrics,
			}},
			"ContainerRestarts": []TestDescription{{
				OutputFilePrefix: "ContainerRestarts",
				Parser:           parseContainerRestarts,
			}},
		},
		"GenericMeasurements": {
			"GenericMeasurements": []TestDescription{{
				OutputFilePrefix:      GenericPrometheusQueryMeasurementName,
				Parser:                parsePerfData,
				ForceConstantCategory: true,
			}},
		},
	}

	// benchmarkDescriptions contains metrics exported by test/integration/scheduler_perf
	benchmarkDescriptions = TestDescriptions{
		"Scheduler": {
			"BenchmarkResults": []TestDescription{{
				Name:             "benchmark",
				OutputFilePrefix: "BenchmarkResults",
				Parser:           parsePerfData,
			}},
			"BenchmarkPerfResults": []TestDescription{{
				// Expected file prefix string is constructed as
				// OutputFilePrefix+"_"+Name. Given we currently generate
				// file prefixed with "BenchmarkPerfScheduling_" followed by a date,
				// set the Name to an empty string.
				Name:             "",
				OutputFilePrefix: "BenchmarkPerfScheduling",
				Parser:           parsePerfData,
			}},
		},
	}

	dnsBenchmarkDescriptions = TestDescriptions{
		"dns": {
			"Latency": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Latency",
				Parser:           parsePerfData,
			}},
			"LatencyPerc": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "LatencyPerc",
				Parser:           parsePerfData,
			}},
			"Queries": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Queries",
				Parser:           parsePerfData,
			}},
			"Qps": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Qps",
				Parser:           parsePerfData,
			}},
		},
	}

	storageDescriptions = TestDescriptions{
		"E2E": {
			"PodStartup": []TestDescription{
				{
					Name:             "storage",
					OutputFilePrefix: "PodStartupLatency_PodWithVolumesStartupLatency",
					Parser:           parsePerfData,
				},
			},
		},
	}

	throughputDescriptions = TestDescriptions{
		"E2E": {
			"PodStartup": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "PodStartupLatency_PodStartupLatency",
					Parser:           parsePerfData,
				},
			},
		},
	}
	windowsDescriptions = TestDescriptions{
		"E2E": {
			"CPUUsage": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsCPUUsagePrometheus",
					Parser:           parsePerfData,
				},
			},
			"MemoryUsage": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsMemoryUsagePrometheus",
					Parser:           parsePerfData,
				},
			},
			"ContainersCount": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsContainers",
					Parser:           parsePerfData,
				},
			},
			"Network": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsNetwork",
					Parser:           parsePerfData,
				},
			},
			"NodeStorage": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsNodeStorage",
					Parser:           parsePerfData,
				},
			},
			"OpenFiles": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsOpenFiles",
					Parser:           parsePerfData,
				},
			},
			"Processes": []TestDescription{
				{
					Name:             "node-throughput",
					OutputFilePrefix: "WindowsProcesses",
					Parser:           parsePerfData,
				},
			},
		},
	}

	jobTypeToDescriptions = map[string]TestDescriptions{
		"performance":  performanceDescriptions,
		"benchmark":    benchmarkDescriptions,
		"dnsBenchmark": dnsBenchmarkDescriptions,
		"storage":      storageDescriptions,
		"throughput":   throughputDescriptions,
		"windows":      windowsDescriptions,
	}
)

// Minimal subset of the prow config definition at k8s.io/test-infra/prow/config
type config struct {
	Periodics []periodic `json:"periodics"`
}
type periodic struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func urlConfigRead(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching prow config from %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading prow config from %s: %v", url, err)
	}
	return b, nil
}

func fileConfigRead(path string) ([]byte, error) {
	return ioutil.ReadFile(path)
}

func getProwConfig(configPaths []string) (Jobs, error) {
	jobs := Jobs{}

	for _, configPath := range configPaths {
		klog.Infof("Fetching config %s", configPath)
		var content []byte
		var err error
		switch {
		case strings.HasPrefix(configPath, "http://"), strings.HasPrefix(configPath, "https://"):
			content, err = urlConfigRead(configPath)
		default:
			content, err = fileConfigRead(configPath)
		}
		if err != nil {
			return nil, err
		}
		conf := &config{}
		if err := yaml.Unmarshal(content, conf); err != nil {
			return nil, fmt.Errorf("error unmarshaling prow config from %s: %v", configPath, err)
		}
		for _, periodic := range conf.Periodics {
			config, err := parsePeriodicConfig(periodic)
			if err != nil {
				klog.Errorf("warning: failed to parse config of %q due to: %v",
					periodic.Name, err)
				continue
			}
			shouldUse, err := checkIfConfigShouldBeUsed(config)
			if err != nil {
				klog.Errorf("warning: failed to validate config of %q due to: %v",
					periodic.Name, err)
				continue
			}
			if shouldUse {
				jobs[periodic.Name] = config
			}
		}
	}
	klog.Infof("Read configs with %d jobs", len(jobs))
	return jobs, nil
}

func parsePeriodicConfig(periodic periodic) (Tests, error) {
	var thisPeriodicConfig Tests
	thisPeriodicConfig.ArtifactsDir = "artifacts"
	for _, tag := range periodic.Tags {
		if strings.HasPrefix(tag, "perfDashPrefix:") {
			split := strings.SplitN(tag, ":", 2)
			thisPeriodicConfig.Prefix = strings.TrimSpace(split[1])
			continue
		}
		if strings.HasPrefix(tag, "perfDashJobType:") {
			split := strings.SplitN(tag, ":", 2)
			jobType := strings.TrimSpace(split[1])
			var exists bool
			if thisPeriodicConfig.Descriptions, exists = jobTypeToDescriptions[jobType]; !exists {
				return Tests{}, fmt.Errorf("unknown job type - %s", jobType)
			}
			continue
		}
		if strings.HasPrefix(tag, "perfDashBuildsCount:") {
			split := strings.SplitN(tag, ":", 2)
			i, err := strconv.Atoi(strings.TrimSpace(split[1]))
			if err != nil {
				return Tests{}, fmt.Errorf("unparsable builds count - %v", split[1])
			}
			if i < 1 {
				return Tests{}, fmt.Errorf("non-positive builds count - %v", i)
			}
			thisPeriodicConfig.BuildsCount = i
			continue
		}
		if strings.HasPrefix(tag, "perfDashArtifactsDir:") {
			split := strings.SplitN(tag, ":", 2)
			thisPeriodicConfig.ArtifactsDir = strings.TrimSpace(split[1])
			continue
		}
		if strings.HasPrefix(tag, "perfDash") {
			return Tests{}, fmt.Errorf("unknown perfdash tag name: %q", tag)
		}
	}
	return thisPeriodicConfig, nil
}

func checkIfConfigShouldBeUsed(config Tests) (bool, error) {
	if config.Prefix == "" && config.Descriptions == nil {
		// This is expected case for jobs which are not expected to be visible in perfdash.
		return false, nil
	}
	if config.Prefix == "" || config.Descriptions == nil {
		return false, fmt.Errorf("none or both of prefix and job type must be specified")
	}
	return true, nil
}
