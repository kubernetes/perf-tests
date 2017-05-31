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
	lowSignificanceLevel     = 0.01
	highSignificanceLevel    = 0.5
	extremeSignificanceLevel = 1.0000001
)

func TestCompareJobsUsingKSTest(t *testing.T) {
	metricKey1 := util.MetricKey{TestName: "swag", Verb: "GET", Resource: "node", Percentile: "Perc99"}
	metricKey2 := util.MetricKey{TestName: "swag", Verb: "PUT", Resource: "pods", Percentile: "Perc90"}
	metricKey3 := util.MetricKey{TestName: "swag", Verb: "LIST", Resource: "rc", Percentile: "Perc100"}
	jobComparisonData := &util.JobComparisonData{
		Data: map[util.MetricKey]*util.MetricComparisonData{
			metricKey1: {
				// Should match even for high significance levels.
				LeftJobSample:  []float64{0.90, 0.95, 1.00, 1.05, 1.10},
				RightJobSample: []float64{0.90, 0.95, 1.00, 1.05, 1.10},
			},
			metricKey2: {
				// Should match only for low significance levels.
				LeftJobSample:  []float64{0.49, 0.50, 0.51},
				RightJobSample: []float64{0.90, 0.95, 1.00, 1.05, 1.10},
			},
			metricKey3: {
				// Should never match as one side of the data is missing.
				LeftJobSample:  []float64{1.00, 10.00, 100.00},
				RightJobSample: []float64{},
				Matched:        false, // Should change to true later.
			},
		},
	}

	// Check that both first and second metric match at a very low significance level.
	CompareJobsUsingKSTest(jobComparisonData, lowSignificanceLevel, 0)
	if !jobComparisonData.Data[metricKey1].Matched || !jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for KS test at a significance level of %v", lowSignificanceLevel)
	}

	// Check that the second metric mismatches at a high significance level, while the first still matches.
	CompareJobsUsingKSTest(jobComparisonData, highSignificanceLevel, 0)
	if !jobComparisonData.Data[metricKey1].Matched || jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for KS test at a significance level of %v", highSignificanceLevel)
	}

	// Checking validity of the statistical test, it should fail always if significance level is > 1.0 (as p-value is always <= 1.0).
	CompareJobsUsingKSTest(jobComparisonData, extremeSignificanceLevel, 0)
	if jobComparisonData.Data[metricKey1].Matched || jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for KS test at a significance level of %v", extremeSignificanceLevel)
	}

	// Checking validity of the test, it should fail as significance level is > 1.0, but it passes due to high enough value of min-metric-avg-for-compare.
	CompareJobsUsingKSTest(jobComparisonData, extremeSignificanceLevel, 1.5)
	if !jobComparisonData.Data[metricKey1].Matched || !jobComparisonData.Data[metricKey2].Matched || !jobComparisonData.Data[metricKey3].Matched {
		t.Errorf("Wrong comparison result for KS test at a significance level of %v with min-metric-avg-for-compare=1.5", extremeSignificanceLevel)
	}
}
