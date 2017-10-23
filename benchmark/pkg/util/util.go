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

package util

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"text/tabwriter"

	"k8s.io/kubernetes/test/e2e/perftype"

	"github.com/golang/glog"
)

// MetricKey is used to identify a metric uniquely.
type MetricKey struct {
	TestName    string // Name of the test ("Load Capacity", "Density", etc)
	Verb        string // "GET","LIST",etc for API calls and "POD STARTUP" for pod startup
	Resource    string // "nodes","pods",etc for API calls and empty value for pod startup
	Subresource string // "status","binding",etc. Empty for pod startup and most API calls
	Scope       string // Used for API calls: "resource" (for GETs), "namespace"/"cluster" (for LISTs).
	Percentile  string // The percentile string ("Perc50", "Perc90", etc)
}

// MetricComparisonData holds all the values corresponding to a metric's comparison.
type MetricComparisonData struct {
	LeftJobSample  []float64 // Sample values from the left job's runs
	RightJobSample []float64 // Sample values from the right job's runs
	Matched        bool      // Boolean indicating if the samples matched
	Comments       string    // Any comments wrt the matching (for human interpretation)

	// Below are some common statistical measures, that we would compute for the left
	// and right job samples. They are used by some comparison schemes.
	AvgL, AvgR, AvgRatio float64 // Average
	StDevL, StDevR       float64 // Standard deviation
	MaxL, MaxR           float64 // Max value
}

// JobComparisonData is a struct holding a map with keys as the metrics' keys and
// values as their comparison data.
type JobComparisonData struct {
	Data map[MetricKey]*MetricComparisonData
}

// MetricFilterFunc tells if a given MetricKey is to be filtered out.
type MetricFilterFunc func(MetricKey, MetricComparisonData) bool

// NewJobComparisonData is a constructor for JobComparisonData struct.
func NewJobComparisonData() *JobComparisonData {
	return &JobComparisonData{
		Data: make(map[MetricKey]*MetricComparisonData),
	}
}

type metricKeyDataPair struct {
	metricKey  MetricKey
	metricData *MetricComparisonData
}

type metricKeyDataPairList []metricKeyDataPair

// We define these functions to implement sort interface on metricKeyDataPairList.
func (metricsList metricKeyDataPairList) Len() int {
	return len(metricsList)
}
func (metricsList metricKeyDataPairList) Less(i, j int) bool {
	if math.IsNaN(metricsList[i].metricData.AvgRatio) {
		return true
	}
	if math.IsNaN(metricsList[j].metricData.AvgRatio) {
		return false
	}
	return metricsList[i].metricData.AvgRatio <= metricsList[j].metricData.AvgRatio
}
func (metricsList metricKeyDataPairList) Swap(i, j int) {
	metricsList[i], metricsList[j] = metricsList[j], metricsList[i]
}

func getMetricsSortedByAvgRatio(j *JobComparisonData) metricKeyDataPairList {
	metricsList := make(metricKeyDataPairList, len(j.Data))
	i := 0
	for metricKey, metricData := range j.Data {
		metricsList[i] = metricKeyDataPair{metricKey, metricData}
		i++
	}
	sort.Sort(sort.Reverse(metricsList))
	return metricsList
}

// PrettyPrintWithFilter prints the job comparison data in a table with columns aligned,
// after sorting the metrics by their avg ratio and removing entries based on filter.
func (j *JobComparisonData) PrettyPrintWithFilter(filter MetricFilterFunc) {
	metricsList := getMetricsSortedByAvgRatio(j)
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "E2E TEST\tVERB\tRESOURCE\tSUBRESOURCE\tSCOPE\tPERCENTILE\tCOMMENTS\n")
	for _, metricPair := range metricsList {
		key, data := metricPair.metricKey, metricPair.metricData
		if filter(key, *data) {
			continue
		}
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n", key.TestName, key.Verb, key.Resource, key.Subresource, key.Scope, key.Percentile, data.Comments)
	}
	w.Flush()
	glog.Infof("\n%v", buf.String())
}

// PrettyPrint prints the job comparison data in a table without any filtering.
func (j *JobComparisonData) PrettyPrint() {
	j.PrettyPrintWithFilter(func(k MetricKey, d MetricComparisonData) bool { return false })
}

// Adds a sample value (if not NaN) to a given metric's MetricComparisonData.
func (j *JobComparisonData) addSampleValue(sample float64, testName, verb, resource, subresource, scope, percentile string, fromLeftJob bool) {
	if math.IsNaN(sample) {
		return
	}
	// Check if the metric exists in the map already, and add it if necessary.
	metricKey := MetricKey{testName, verb, resource, subresource, scope, percentile}
	if _, ok := j.Data[metricKey]; !ok {
		j.Data[metricKey] = &MetricComparisonData{}
	}
	// Add the sample to the metric's comparison data.
	if fromLeftJob {
		j.Data[metricKey].LeftJobSample = append(j.Data[metricKey].LeftJobSample, sample)
	} else {
		j.Data[metricKey].RightJobSample = append(j.Data[metricKey].RightJobSample, sample)
	}
}

func (j *JobComparisonData) addLatencyValue(latency *perftype.DataItem, minAllowedRequestCount int, testName string, fromLeftJob bool) {
	if latency.Labels["Count"] != "" {
		if count, err := strconv.Atoi(latency.Labels["Count"]); err != nil || count < minAllowedRequestCount {
			return
		}
	}
	verb := latency.Labels["Verb"]
	resource := latency.Labels["Resource"]
	subresource := latency.Labels["Subresource"]
	scope := latency.Labels["Scope"]
	if latency.Labels["Metric"] == "pod_startup" {
		verb = "Pod-Startup"
	}
	for percentile, value := range latency.Data {
		j.addSampleValue(value, testName, verb, resource, subresource, scope, percentile, fromLeftJob)
	}
}

// GetFlattennedComparisonData flattens latencies from various runs of left & right jobs into JobComparisonData.
// In the process, it also discards those metric samples with request count less than minAllowedAPIRequestCount.
func GetFlattennedComparisonData(leftJobMetrics, rightJobMetrics []map[string][]perftype.PerfData, minAllowedAPIRequestCount int) *JobComparisonData {
	j := NewJobComparisonData()
	for _, singleRunMetrics := range leftJobMetrics {
		for testName, latenciesArray := range singleRunMetrics {
			for _, latencies := range latenciesArray {
				for _, latency := range latencies.DataItems {
					j.addLatencyValue(&latency, minAllowedAPIRequestCount, testName, true)
				}
			}
		}
	}
	for _, singleRunMetrics := range rightJobMetrics {
		for testName, latenciesArray := range singleRunMetrics {
			for _, latencies := range latenciesArray {
				for _, latency := range latencies.DataItems {
					j.addLatencyValue(&latency, minAllowedAPIRequestCount, testName, false)
				}
			}
		}
	}
	return j
}

func computeSampleStats(sample []float64, avg, stDev, max *float64) {
	len := len(sample)
	if len == 0 {
		*avg = math.NaN()
		*stDev = math.NaN()
		*max = math.NaN()
		return
	}
	sum := 0.0
	squareSum := 0.0
	for i := 0; i < len; i++ {
		sum += sample[i]
		squareSum += sample[i] * sample[i]
		*max = math.Max(*max, sample[i])
	}
	*avg = sum / float64(len)
	*stDev = math.Sqrt(squareSum/float64(len) - (*avg * *avg))
}

// ComputeStatsForMetricSamples computes avg, std-dev and max for each metric's left and right samples.
func (j *JobComparisonData) ComputeStatsForMetricSamples() {
	for _, metricData := range j.Data {
		computeSampleStats(metricData.LeftJobSample, &metricData.AvgL, &metricData.StDevL, &metricData.MaxL)
		computeSampleStats(metricData.RightJobSample, &metricData.AvgR, &metricData.StDevR, &metricData.MaxR)
	}
}
