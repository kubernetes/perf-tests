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
	"os"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
)

// To add new e2e test support, you need to:
//   1) Transform e2e performance test result into *PerfData* in k8s/kubernetes/test/e2e/perftype,
//   and print the PerfData in e2e test log.
//   2) Add corresponding bucket, job and test into *TestConfig*.

// TestDescription contains test name, output file prefix and parser function.
type TestDescription struct {
	Name             string
	OutputFilePrefix string
	Parser           func(data []byte, buildNumber int, testResult *BuildData)
}

// TestDescriptions is a map job->component->description.
type TestDescriptions map[string]map[string][]TestDescription

// Tests is a map from test label to test description.
type Tests struct {
	Prefix       string
	Descriptions TestDescriptions
	BuildsCount  int
}

// Jobs is a map from job name to all supported tests in the job.
type Jobs map[string]Tests

// Buckets is a map from bucket url to all supported jobs in the bucket.
type Buckets map[string]Jobs

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
			"DensityPodStartup": []TestDescription{
				{
					Name:             "density",
					OutputFilePrefix: "PodStartupLatency",
					Parser:           parseResponsivenessData,
				},
				{
					Name:             "density",
					OutputFilePrefix: "PodStartupLatency_PodStartupLatency",
					Parser:           parseResponsivenessData,
				},
			},
			"DensitySaturationPodStartup": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "PodStartupLatency_SaturationPodStartupLatency",
				Parser:           parseResponsivenessData,
			}},
			"DensityTestPhaseTimer": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "TestPhaseTimer",
				Parser:           parseResponsivenessData,
			}},
			"LoadResources": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "ResourceUsageSummary",
				Parser:           parseResourceUsageData,
			}},
			"LoadTestPhaseTimer": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "TestPhaseTimer",
				Parser:           parseResponsivenessData,
			}},
		},
		"APIServer": {
			"DensityResponsiveness": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseResponsivenessData,
			}},
			"DensityRequestCount": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseRequestCountData,
			}},
			"DensityRequestCountByClient": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverRequestCount,
			}},
			"LoadResponsiveness": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseResponsivenessData,
			}},
			"LoadRequestCount": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "APIResponsiveness",
				Parser:           parseRequestCountData,
			}},
			"LoadRequestCountByClient": []TestDescription{{
				Name:             "load",
				OutputFilePrefix: "MetricsForE2E",
				Parser:           parseApiserverRequestCount,
			}},
		},
		"Scheduler": {
			"SchedulingLatency": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "SchedulingMetrics",
				Parser:           parseSchedulingLatency,
			}},
			"SchedulingThroughput": []TestDescription{
				{
					Name:             "density",
					OutputFilePrefix: "SchedulingThroughput",
					Parser:           parseSchedulingThroughputCL,
				},
				{
					Name:             "density",
					OutputFilePrefix: "SchedulingMetrics",
					Parser:           parseSchedulingThroughput,
				},
			},
		},
		"Etcd": {
			"BackendCommitDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("backendCommitDuration"),
			}},
			"SnapshotSaveTotalDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("snapshotSaveTotalDuration"),
			}},
			"PeerRoundTripTime": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("peerRoundTripTime"),
			}},
			"WalFsyncDuration": []TestDescription{{
				Name:             "density",
				OutputFilePrefix: "EtcdMetrics",
				Parser:           parseHistogramMetric("walFsyncDuration"),
			}},
		},
	}

	// benchmarkDescriptions contains metrics exported by test/integration/scheduler_perf
	benchmarkDescriptions = TestDescriptions{
		"Scheduler": {
			"BenchmarkResults": []TestDescription{{
				Name:             "benchmark",
				OutputFilePrefix: "BenchmarkResults",
				Parser:           parseResponsivenessData,
			}},
		},
	}

	dnsBenchmarkDescriptions = TestDescriptions{
		"dns": {
			"Latency": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Latency",
				Parser:           parseResponsivenessData,
			}},
			"LatencyPerc": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "LatencyPerc",
				Parser:           parseResponsivenessData,
			}},
			"Queries": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Queries",
				Parser:           parseResponsivenessData,
			}},
			"Qps": []TestDescription{{
				Name:             "dns",
				OutputFilePrefix: "Qps",
				Parser:           parseResponsivenessData,
			}},
		},
	}

	jobTypeToDescriptions = map[string]TestDescriptions{
		"performance":  performanceDescriptions,
		"benchmark":    benchmarkDescriptions,
		"dnsBenchmark": dnsBenchmarkDescriptions,
	}
)

func getProwConfigOrDie(configPaths []string) Jobs {
	jobs, err := getProwConfig(configPaths)
	if err != nil {
		panic(err)
	}
	return jobs
}

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
		fmt.Fprintf(os.Stderr, "Fetching config %s\n", configPath)
		// Perfdash supports only yamls.
		if !strings.HasSuffix(configPath, ".yaml") {
			fmt.Fprintf(os.Stderr, "%s is not an yaml file!\n", configPath)
			continue
		}
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
			var thisPeriodicConfig Tests
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
						fmt.Fprintf(os.Stderr, "warning: unknown job type - %s\n", jobType)
					}
					continue
				}
				if strings.HasPrefix(tag, "perfDashBuildsCount:") {
					split := strings.SplitN(tag, ":", 2)
					i, err := strconv.Atoi(strings.TrimSpace(split[1]))
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: unparsable builds count - %v\n", split[1])
					}
					if i < 1 {
						fmt.Fprintf(os.Stderr, "warning: non-positive builds count - %v\n", i)
						continue
					}
					thisPeriodicConfig.BuildsCount = i
					continue
				}
				if strings.HasPrefix(tag, "perfDash") {
					fmt.Fprintf(os.Stderr, "warning: unknown perfdash tag name: %q\n", tag)
				}
			}
			if thisPeriodicConfig.Prefix == "" && thisPeriodicConfig.Descriptions == nil {
				continue
			}
			if thisPeriodicConfig.Prefix == "" || thisPeriodicConfig.Descriptions == nil {
				return nil, fmt.Errorf("invalid perfdash config of periodic %q: none or both of prefix and job type must be specified", periodic.Name)
			}
			jobs[periodic.Name] = thisPeriodicConfig
		}
	}
	fmt.Printf("Read configs with %d jobs\n", len(jobs))
	return jobs, nil
}
