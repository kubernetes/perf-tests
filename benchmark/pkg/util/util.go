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
	"text/tabwriter"

	e2e "k8s.io/kubernetes/test/e2e/framework"

	"github.com/golang/glog"
)

// MetricKey is used to identify a metric uniquely.
type MetricKey struct {
	TestName   string // Name of the test ("Load Capacity", "Density", etc)
	Verb       string // "GET","LIST",etc for API calls and "POD STARTUP" for pod startup
	Resource   string // "nodes","pods", etc for API calls and empty value for pod startup
	Percentile string // The percentile string ("Perc50", "Perc90", etc)
}

// MetricComparisonData holds all the values corresponding to a metric's comparison.
type MetricComparisonData struct {
	LeftJobSample  []float64 // Sample values from the left job's runs
	RightJobSample []float64 // Sample values from the right job's runs
	Matched        bool      // Boolean indicating if the samples matched
	Comments       string    // Any comments wrt the matching (for human interpretation)

	// Below are some common statistical measures, that we would compute for the left
	// and right job samples. They are used by some comparison schemes.
	AvgL, AvgR     float64 // Average
	StDevL, StDevR float64 // Standard deviation
	MaxL, MaxR     float64 // Max value
}

// JobComparisonData is a struct holding a map with keys as the metrics' keys and
// values as their comparison data.
type JobComparisonData struct {
	Data map[MetricKey]*MetricComparisonData
}

// NewJobComparisonData is a constructor for JobComparisonData struct.
func NewJobComparisonData() *JobComparisonData {
	return &JobComparisonData{
		Data: make(map[MetricKey]*MetricComparisonData),
	}
}

// PrettyPrint prints the job comparison data in a table form with columns aligned.
func (j *JobComparisonData) PrettyPrint() {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "E2E TEST\tVERB\tRESOURCE\tPERCENTILE\tMATCHED?\tCOMMENTS\n")
	for key, data := range j.Data {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\n", key.TestName, key.Verb, key.Resource, key.Percentile, data.Matched, data.Comments)
	}
	w.Flush()
	glog.Infof("\n%v", buf.String())
}

// Adds a sample value (if not NaN) to a given metric's MetricComparisonData.
func (j *JobComparisonData) addSampleValue(sample float64, testName, verb, resource, percentile string, fromLeftJob bool) {
	if math.IsNaN(sample) {
		return
	}
	// Check if the metric exists in the map already, and add it if necessary.
	metricKey := MetricKey{testName, verb, resource, percentile}
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

func (j *JobComparisonData) addAPICallLatencyValues(apiCall *e2e.APICall, testName string, fromLeftJob bool) {
	perc50 := float64(apiCall.Latency.Perc50)
	perc90 := float64(apiCall.Latency.Perc90)
	perc99 := float64(apiCall.Latency.Perc99)
	j.addSampleValue(perc50, testName, apiCall.Verb, apiCall.Resource, "Perc50", fromLeftJob)
	j.addSampleValue(perc90, testName, apiCall.Verb, apiCall.Resource, "Perc90", fromLeftJob)
	j.addSampleValue(perc99, testName, apiCall.Verb, apiCall.Resource, "Perc99", fromLeftJob)
}

func (j *JobComparisonData) addPodStartupLatencyValues(podStartupLatency *e2e.PodStartupLatency, testName string, fromLeftJob bool) {
	perc50 := float64(podStartupLatency.Latency.Perc50)
	perc90 := float64(podStartupLatency.Latency.Perc90)
	perc99 := float64(podStartupLatency.Latency.Perc99)
	perc100 := float64(podStartupLatency.Latency.Perc100)
	j.addSampleValue(perc50, testName, "POD_STARTUP", "", "Perc50", fromLeftJob)
	j.addSampleValue(perc90, testName, "POD_STARTUP", "", "Perc90", fromLeftJob)
	j.addSampleValue(perc99, testName, "POD_STARTUP", "", "Perc99", fromLeftJob)
	j.addSampleValue(perc100, testName, "POD_STARTUP", "", "Perc100", fromLeftJob)
}

// GetFlattennedComparisonData flattens arrays of API and pod latencies of left & right jobs into JobComparisonData.
func GetFlattennedComparisonData(leftApiLatencies, rightApiLatencies []map[string]*e2e.APIResponsiveness,
	leftPodLatencies, rightPodLatencies []map[string]*e2e.PodStartupLatency) *JobComparisonData {
	j := NewJobComparisonData()
	// Add API call latencies of left job.
	for _, runApiLatencies := range leftApiLatencies {
		for testName, apiCallLatencies := range runApiLatencies {
			for _, apiCallLatency := range apiCallLatencies.APICalls {
				j.addAPICallLatencyValues(&apiCallLatency, testName, true)
			}
		}
	}
	// Add API call latencies of right job.
	for _, runApiLatencies := range rightApiLatencies {
		for testName, apiCallLatencies := range runApiLatencies {
			for _, apiCallLatency := range apiCallLatencies.APICalls {
				j.addAPICallLatencyValues(&apiCallLatency, testName, false)
			}
		}
	}
	// Add Pod startup latencies of left job.
	for _, runPodLatencies := range leftPodLatencies {
		for testName, podStartupLatency := range runPodLatencies {
			j.addPodStartupLatencyValues(podStartupLatency, testName, true)
		}
	}
	// Add Pod startup latencies of right job.
	for _, runPodLatencies := range rightPodLatencies {
		for testName, podStartupLatency := range runPodLatencies {
			j.addPodStartupLatencyValues(podStartupLatency, testName, false)
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
