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
	"math"
	"reflect"
	"testing"

	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func TestGetFlattennedComparisonData(t *testing.T) {
	leftAPILatencies := []map[string]*e2e.APIResponsiveness{
		{
			// Metrics from 1st run.
			"Load": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "node",
						Verb:     "GET",
						Latency: e2e.LatencyMetric{
							Perc50: 434506,
							Perc90: 17499,
							Perc99: 360726,
						},
					},
					{
						Resource: "pod",
						Verb:     "POST",
						Latency: e2e.LatencyMetric{
							Perc50: 708401,
							Perc90: 99265,
							Perc99: 889297,
						},
					},
				},
			},
			"Density": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "service",
						Verb:     "DELETE",
						Latency: e2e.LatencyMetric{
							Perc50: 560427,
							Perc90: 735918,
							Perc99: 725196,
						},
					},
				},
			},
		},
		{
			// Metrics from 2nd run.
			"Load": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "node",
						Verb:     "GET",
						Latency: e2e.LatencyMetric{
							Perc50: 385699,
							Perc90: 181956,
							Perc99: 564837,
						},
					},
				},
			},
		},
	}

	rightAPILatencies := []map[string]*e2e.APIResponsiveness{
		{
			// Metrics from 1st run.
			"Load": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "node",
						Verb:     "GET",
						Latency: e2e.LatencyMetric{
							Perc50: 540908,
							Perc90: 130667,
							Perc99: 898554,
						},
					},
				},
			},
			"Density": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "service",
						Verb:     "DELETE",
						Latency: e2e.LatencyMetric{
							Perc50: 781639,
							Perc90: 741522,
							Perc99: 284668,
						},
					},
				},
			},
		},
		{
			// Metrics from 2nd run.
			"Load": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "node",
						Verb:     "GET",
						Latency: e2e.LatencyMetric{
							Perc50: 587656,
							Perc90: 899073,
							Perc99: 29665,
						},
					},
				},
			},
			"Density": &e2e.APIResponsiveness{
				APICalls: []e2e.APICall{
					{
						Resource: "service",
						Verb:     "DELETE",
						Latency: e2e.LatencyMetric{
							Perc50: 370847,
							Perc90: 843692,
							Perc99: 763390,
						},
					},
				},
			},
		},
	}

	leftPodLatencies := []map[string]*e2e.PodStartupLatency{
		{
			// Metrics from 1st run.
			"Load": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  110369,
					Perc90:  918387,
					Perc99:  602585,
					Perc100: 843511,
				},
			},
			"Density": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  110369,
					Perc90:  918387,
					Perc99:  602585,
					Perc100: 843511,
				},
			},
		},
		{
			// Metrics from 2nd run.
			"Load": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  692132,
					Perc90:  697577,
					Perc99:  944434,
					Perc100: 32134,
				},
			},
			"Density": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  855293,
					Perc90:  647678,
					Perc99:  886836,
					Perc100: 668049,
				},
			},
		},
	}

	rightPodLatencies := []map[string]*e2e.PodStartupLatency{
		{
			// Metrics from 1st run.
			"Load": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  975403,
					Perc90:  286765,
					Perc99:  137867,
					Perc100: 905950,
				},
			},
			"Density": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  247128,
					Perc90:  463653,
					Perc99:  180198,
					Perc100: 164989,
				},
			},
		},
		{
			// Metrics from 2nd run.
			"Load": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  270962,
					Perc90:  588448,
					Perc99:  549149,
					Perc100: 811366,
				},
			},
			"Density": &e2e.PodStartupLatency{
				Latency: e2e.LatencyMetric{
					Perc50:  774048,
					Perc90:  810676,
					Perc99:  532709,
					Perc100: 200269,
				},
			},
		},
	}

	expectedJobComparisonData := &JobComparisonData{
		Data: map[MetricKey]*MetricComparisonData{
			{
				TestName:   "Load",
				Verb:       "GET",
				Resource:   "node",
				Percentile: "Perc50",
			}: {
				LeftJobSample:  []float64{434506, 385699},
				RightJobSample: []float64{540908, 587656},
			},
			{
				TestName:   "Load",
				Verb:       "GET",
				Resource:   "node",
				Percentile: "Perc90",
			}: {
				LeftJobSample:  []float64{17499, 181956},
				RightJobSample: []float64{130667, 899073},
			},
			{
				TestName:   "Load",
				Verb:       "GET",
				Resource:   "node",
				Percentile: "Perc99",
			}: {
				LeftJobSample:  []float64{360726, 564837},
				RightJobSample: []float64{898554, 29665},
			},
			{
				TestName:   "Load",
				Verb:       "POST",
				Resource:   "pod",
				Percentile: "Perc50",
			}: {
				LeftJobSample:  []float64{708401},
				RightJobSample: nil,
			},
			{
				TestName:   "Load",
				Verb:       "POST",
				Resource:   "pod",
				Percentile: "Perc90",
			}: {
				LeftJobSample:  []float64{99265},
				RightJobSample: nil,
			},
			{
				TestName:   "Load",
				Verb:       "POST",
				Resource:   "pod",
				Percentile: "Perc99",
			}: {
				LeftJobSample:  []float64{889297},
				RightJobSample: nil,
			},
			{
				TestName:   "Density",
				Verb:       "DELETE",
				Resource:   "service",
				Percentile: "Perc50",
			}: {
				LeftJobSample:  []float64{560427},
				RightJobSample: []float64{781639, 370847},
			},
			{
				TestName:   "Density",
				Verb:       "DELETE",
				Resource:   "service",
				Percentile: "Perc90",
			}: {
				LeftJobSample:  []float64{735918},
				RightJobSample: []float64{741522, 843692},
			},
			{
				TestName:   "Density",
				Verb:       "DELETE",
				Resource:   "service",
				Percentile: "Perc99",
			}: {
				LeftJobSample:  []float64{725196},
				RightJobSample: []float64{284668, 763390},
			},
			{
				TestName:   "Load",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc50",
			}: {
				LeftJobSample:  []float64{110369, 692132},
				RightJobSample: []float64{975403, 270962},
			},
			{
				TestName:   "Load",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc90",
			}: {
				LeftJobSample:  []float64{918387, 697577},
				RightJobSample: []float64{286765, 588448},
			},
			{
				TestName:   "Load",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc99",
			}: {
				LeftJobSample:  []float64{602585, 944434},
				RightJobSample: []float64{137867, 549149},
			},
			{
				TestName:   "Load",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc100",
			}: {
				LeftJobSample:  []float64{843511, 32134},
				RightJobSample: []float64{905950, 811366},
			},
			{
				TestName:   "Density",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc50",
			}: {
				LeftJobSample:  []float64{110369, 855293},
				RightJobSample: []float64{247128, 774048},
			},
			{
				TestName:   "Density",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc90",
			}: {
				LeftJobSample:  []float64{918387, 647678},
				RightJobSample: []float64{463653, 810676},
			},
			{
				TestName:   "Density",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc99",
			}: {
				LeftJobSample:  []float64{602585, 886836},
				RightJobSample: []float64{180198, 532709},
			},
			{
				TestName:   "Density",
				Verb:       "POD_STARTUP",
				Resource:   "",
				Percentile: "Perc100",
			}: {
				LeftJobSample:  []float64{843511, 668049},
				RightJobSample: []float64{164989, 200269},
			},
		},
	}

	jobComparisonData := GetFlattennedComparisonData(leftAPILatencies, rightAPILatencies, leftPodLatencies, rightPodLatencies)
	if !reflect.DeepEqual(*jobComparisonData, *expectedJobComparisonData) {
		t.Errorf("Flattenned comparison data mismatched from what was expected")
	}
}

func TestComputeStatsForMetricSamples(t *testing.T) {
	metricKey := MetricKey{TestName: "xyz", Verb: "foo", Resource: "bar", Percentile: "foobar"}
	jobComparisonData := &JobComparisonData{
		Data: map[MetricKey]*MetricComparisonData{
			metricKey: {
				LeftJobSample:  []float64{1.0, 2.0, 3.0, 4.0, 5.0},
				RightJobSample: nil,
			},
		},
	}
	jobComparisonData.ComputeStatsForMetricSamples()

	// Check that the avg, stddev and max have been correctly computed.
	if !math.IsNaN(jobComparisonData.Data[metricKey].AvgR) ||
		!math.IsNaN(jobComparisonData.Data[metricKey].StDevR) ||
		!math.IsNaN(jobComparisonData.Data[metricKey].MaxR) {
		t.Errorf("Computed stats (avg/SD/max) not NaN when array is empty")
	}
	if math.Abs(jobComparisonData.Data[metricKey].AvgL-3.0) > 0.00001 {
		t.Errorf("Average computed as %v, but expected 3.0", jobComparisonData.Data[metricKey].AvgL)
	}
	if math.Abs(jobComparisonData.Data[metricKey].StDevL-1.41421) > 0.00001 {
		t.Errorf("Std. deviation computed as %v, but expected 1.41421", jobComparisonData.Data[metricKey].StDevL)
	}
	if jobComparisonData.Data[metricKey].MaxL != 5.0 {
		t.Errorf("Max computed as %v, but expected 5.0", jobComparisonData.Data[metricKey].MaxL)
	}
}
