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

package schemes

import (
	"testing"

	"k8s.io/perf-tests/benchmark/pkg/util"
)

const (
	lowAvgRatioThreshold    = 0.4
	mediumAvgRatioThreshold = 0.8
	highAvgRatioThreshold   = 0.9
)

func TestCompareJobsUsingAvgTest(t *testing.T) {
	metricKey1 := util.MetricKey{TestName: "swag", Verb: "GET", Resource: "node", Percentile: "Perc99"}
	metricKey2 := util.MetricKey{TestName: "swag", Verb: "PUT", Resource: "pods", Percentile: "Perc90"}
	metricKey3 := util.MetricKey{TestName: "swag", Verb: "LIST", Resource: "rc", Percentile: "Perc100"}
	jobComparisonData := &util.JobComparisonData{
		Data: map[util.MetricKey]*util.MetricComparisonData{
			metricKey1: {
				LeftJobSample:  []float64{0.90, 0.95, 1.00, 1.05, 1.10},
				RightJobSample: []float64{0.79, 0.80, 0.81, 0.82},
			},
			metricKey2: {
				LeftJobSample:  []float64{0.49, 0.50, 0.51},
				RightJobSample: []float64{0.90, 0.95, 1.00, 1.05, 1.10},
			},
			metricKey3: {
				LeftJobSample:  []float64{1.00, 10.00, 100.00},
				RightJobSample: []float64{},
				Matched:        false, // Should change to true later.
			},
		},
	}

	CompareJobsUsingAvgTest(jobComparisonData, lowAvgRatioThreshold, 0)
	if !jobComparisonData.Data[metricKey1].Matched || !jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for Avg-based test at an allowed ratio of %v", lowAvgRatioThreshold)
	}

	CompareJobsUsingAvgTest(jobComparisonData, mediumAvgRatioThreshold, 0)
	if !jobComparisonData.Data[metricKey1].Matched || jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for Avg-based test at an allowed ratio of %v", mediumAvgRatioThreshold)
	}

	CompareJobsUsingAvgTest(jobComparisonData, highAvgRatioThreshold, 0)
	if jobComparisonData.Data[metricKey1].Matched || jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for Avg-based test at an allowed ratio of %v", highAvgRatioThreshold)
	}

	CompareJobsUsingAvgTest(jobComparisonData, highAvgRatioThreshold, 1.5)
	if !jobComparisonData.Data[metricKey1].Matched || !jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for Avg-based test at an allowed ratio of %v with min-metric-avg-for-compare=1.5", highAvgRatioThreshold)
	}
}
