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

package main

import (
	"flag"
	"fmt"

	"k8s.io/perf-tests/benchmark/pkg/comparer"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/runselector"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/scraper"
	mfutil "k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
	"k8s.io/perf-tests/benchmark/pkg/util"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

var (
	leftJobName, rightJobName string
	logSourceMode             string
	runSelectionScheme        string
	nHoursCount               int
	nRunsCount                int
	minAllowedAPIRequestCount int
	comparisonScheme          string
	matchThreshold            float64
	minMetricAvgForCompare    float64
)

func registerFlags(fs *pflag.FlagSet) {
	fs.StringVar(&leftJobName, "left-job-name", "ci-kubernetes-e2e-gci-gce-scalability", "Name of the job to be used as left hand side of comparison")
	fs.StringVar(&rightJobName, "right-job-name", "ci-kubernetes-kubemark-100-gce", "Name of the job to be used as right hand side of comparison")
	fs.StringVar(&logSourceMode, "log-source-mode", "gcs", fmt.Sprintf("Source mode for getting the logs. Allowed options: %v", mfutil.GCS))
	fs.StringVar(&runSelectionScheme, "run-selection-scheme", runselector.LastNHours, fmt.Sprintf("Scheme for selecting the runs to be compared. Allowed options: %v, %v", runselector.LastNHours, runselector.LastNRuns))
	fs.IntVar(&nHoursCount, "n-hours-count", 24, "Value of 'n' to use in the last-n-hours run-selection scheme")
	fs.IntVar(&nRunsCount, "n-runs-count", 20, "Value of 'n' to use in the last-n-runs run-selection scheme")
	fs.IntVar(&minAllowedAPIRequestCount, "min-allowed-api-request-count", 10, "The minimum requests count for an API call (within a particular test of a particular run) to be included for comparison")
	fs.StringVar(&comparisonScheme, "comparison-scheme", comparer.AvgTest, fmt.Sprintf("Statistical test to be used as the algorithm for comparison. Allowed options: %v, %v", comparer.AvgTest, comparer.KSTest))
	fs.Float64Var(&matchThreshold, "match-threshold", 0.66, "The threshold for metric comparison, interpretation depends on test used (significance level for KSTest, bound for ratio of avgs in AvgTest)")
	fs.Float64Var(&minMetricAvgForCompare, "min-metric-avg-for-compare", 50.0, "The minimum value for a metric's avg to consider it for comparison. If in both left & right job the avg is less than this, it's directly marked as matched.")
}

// Select the runs of the left and right jobs to be used for comparison using the given run-selection scheme.
func selectRuns() ([]int, []int) {
	utils, err := mfutil.GetJobLogUtilsForMode(logSourceMode)
	if err != nil {
		glog.Fatalf("Couldn't obtain log utils: %v", err)
	}

	nForRunSelection := nHoursCount
	if runSelectionScheme == runselector.LastNRuns {
		nForRunSelection = nRunsCount
	}
	glog.Infof("Selecting runs for jobs '%v' and '%v' using scheme '%v' with n = %v", leftJobName, rightJobName, runSelectionScheme, nForRunSelection)

	leftJobRuns, err := runselector.GetJobRunsUsingScheme(leftJobName, runSelectionScheme, nForRunSelection, utils)
	if err != nil {
		glog.Fatalf("Couldn't select runs for left job: %v", err)
	}
	glog.Infof("Runs selected for job %v: %v", leftJobName, leftJobRuns)

	rightJobRuns, err := runselector.GetJobRunsUsingScheme(rightJobName, runSelectionScheme, nForRunSelection, utils)
	if err != nil {
		glog.Fatalf("Couldn't select runs: %v", err)
	}
	glog.Infof("Runs selected for job %v: %v", rightJobName, rightJobRuns)

	return leftJobRuns, rightJobRuns
}

// Obtain the metrics for the left and right job runs provided.
func getMetrics(leftJobRuns, rightJobRuns []int) *util.JobComparisonData {
	utils, err := mfutil.GetJobLogUtilsForMode(logSourceMode)
	if err != nil {
		glog.Fatalf("Couldn't obtain log utils: %v", err)
	}

	glog.Infof("Fetching metrics for the chosen runs of job %v", leftJobName)
	leftJobLatencyMetrics := scraper.GetMetricsForRuns(leftJobName, leftJobRuns, utils)
	if leftJobLatencyMetrics == nil {
		glog.Fatalf("Could not collect metrics even for a single run of the job")
	}

	glog.Infof("Fetching metrics for the chosen runs of job %v", rightJobName)
	rightJobLatencyMetrics := scraper.GetMetricsForRuns(rightJobName, rightJobRuns, utils)
	if rightJobLatencyMetrics == nil {
		glog.Fatalf("Could not collect metrics even for a single run of the job")
	}

	glog.Infof("Flattening the metrics maps into per-metric structs")
	jobComparisonData := util.GetFlattennedComparisonData(leftJobLatencyMetrics, rightJobLatencyMetrics, minAllowedAPIRequestCount)
	return jobComparisonData
}

// Compare jobs using the metrics data given with the chosen comparison scheme.
func compare(jobComparisonData *util.JobComparisonData) {
	glog.Infof("Comparing metrics for the jobs using scheme '%v' at a threshold value of %v (with min-metric-avg-for-compare=%v)", comparisonScheme, matchThreshold, minMetricAvgForCompare)
	err := comparer.CompareJobsUsingScheme(jobComparisonData, comparisonScheme, matchThreshold, minMetricAvgForCompare)
	if err != nil {
		glog.Fatalf("Failed to compare the jobs: %v", err)
	}
}

// Pretty print results of the comparison.
func printResults(jobComparisonData *util.JobComparisonData) {
	glog.Infof("Comparison results for 99th percentile of latency metrics:")
	glog.Infof("Mismatched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc99" || d.Matched
	})
	glog.Infof("")
	glog.Infof("Matched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc99" || !d.Matched
	})
	glog.Infof("")
	glog.Infof("Comparison results for 90th percentile of latency metrics:")
	glog.Infof("Mismatched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc90" || d.Matched
	})
	glog.Infof("")
	glog.Infof("Matched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc90" || !d.Matched
	})
	glog.Infof("")
	glog.Infof("Comparison results for 50th percentile of latency metrics:")
	glog.Infof("Mismatched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc50" || d.Matched
	})
	glog.Infof("")
	glog.Infof("Matched metrics:")
	jobComparisonData.PrettyPrintWithFilter(func(k util.MetricKey, d util.MetricComparisonData) bool {
		return k.Percentile != "Perc50" || !d.Matched
	})
}

func main() {
	// Set the tool's flags.
	registerFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	// Perform comparison.
	leftJobRuns, rightJobRuns := selectRuns()
	jobComparisonData := getMetrics(leftJobRuns, rightJobRuns)
	compare(jobComparisonData)
	printResults(jobComparisonData)
}
