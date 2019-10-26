/*
Copyright 2017 The Kubernetes Authors.

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

package scraper

import (
	"encoding/json"
	"strings"

	"k8s.io/kubernetes/test/e2e/perftype"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"

	"github.com/golang/glog"
)

// Path prefixes for the metrics files we want to scrape.
const (
	APICallLatencyFilePrefix    = "artifacts/APIResponsiveness_"
	PodStartupLatencyFilePrefix = "artifacts/PodStartupLatency_"
)

// GetMetricsFilePathsForRun for a given run of a job, returns a map of testname ("load", "density", etc) to
// a list of paths to its latency files (API responsiveness, pod startup) relative to the run's root dir.
func GetMetricsFilePathsForRun(job string, run int, utils util.JobLogUtils) map[string][]string {
	latencyFiles := make([]string, 0)
	latencyFilesForTest := make(map[string][]string)

	if apiCallLatencyFiles, err := utils.ListJobRunFilesWithPrefix(job, run, APICallLatencyFilePrefix); err == nil {
		latencyFiles = append(latencyFiles, apiCallLatencyFiles...)
	} else {
		glog.V(0).Infof("Failed to list API call latency files for run %v:%v (skipping them): %v", job, run, err)
	}
	if podStartupLatencyFiles, err := utils.ListJobRunFilesWithPrefix(job, run, PodStartupLatencyFilePrefix); err == nil {
		latencyFiles = append(latencyFiles, podStartupLatencyFiles...)
	} else {
		glog.V(0).Infof("Failed to list pod startup latency files for run %v:%v (skipping them): %v", job, run, err)
	}

	// Group the list of latency files into a map of list of files key'ed by testname.
	for _, latencyFile := range latencyFiles {
		filenameParts := strings.Split(latencyFile, "_")
		if len(filenameParts) < 3 {
			glog.V(0).Infof("Could not get testname from filename '%v' (skipping it)", latencyFile)
			continue
		}
		// TODO(shyamjvs): Handle the case of multiple tests with same name, if needed.
		testName := filenameParts[len(filenameParts)-2]
		relPathStartIndex := strings.LastIndex(latencyFile, "artifacts/")
		latencyFilesForTest[testName] = append(latencyFilesForTest[testName], latencyFile[relPathStartIndex:])
	}
	return latencyFilesForTest
}

// GetMetricsForRun for a given run of a job, returns a map of testname ("load", "density", etc) to a
// list of its latency metrics (API responsiveness, pod startup) in perfType.PerfData format.
func GetMetricsForRun(job string, run int, utils util.JobLogUtils) map[string][]perftype.PerfData {
	metricsForRun := make(map[string][]perftype.PerfData)
	latencyFilesForTest := GetMetricsFilePathsForRun(job, run, utils)

	for testName, latencyFiles := range latencyFilesForTest {
		for _, latencyFile := range latencyFiles {
			latencyFileContents, err := utils.GetJobRunFileContents(job, run, latencyFile)
			if err != nil {
				glog.V(0).Infof("Error reading latency metrics file for run %v:%v (skipping it): %v", job, run, err)
				continue
			}
			perfData := perftype.PerfData{}
			if err := json.Unmarshal(latencyFileContents, &perfData); err != nil {
				glog.V(0).Infof("Error parsing latency metrics file %v for run %v:%v (skipping it): %v", latencyFile, job, run, err)
				continue
			}
			metricsForRun[testName] = append(metricsForRun[testName], perfData)
		}
	}
	return metricsForRun
}

// GetMetricsForRuns is a wrapper for calling GetMetricsForRun on multiple runs returning
// an array of the obtained results. Neglects runs whose metrics could not be fetched.
// Note: This does best-effort scraping, returning as much as could be scraped, without any error.
func GetMetricsForRuns(job string, runs []int, utils util.JobLogUtils) []map[string][]perftype.PerfData {
	var metricsForRuns []map[string][]perftype.PerfData
	for _, run := range runs {
		metricsForRun := GetMetricsForRun(job, run, utils)
		if len(metricsForRun) == 0 {
			glog.V(0).Infof("No metrics obtained at all for run %v:%v (skipping it)", job, run)
			continue
		}
		metricsForRuns = append(metricsForRuns, metricsForRun)
	}
	return metricsForRuns
}
