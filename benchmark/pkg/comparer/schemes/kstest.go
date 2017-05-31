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
	"fmt"
	"math"

	"k8s.io/perf-tests/benchmark/pkg/util"

	"github.com/dgryski/go-onlinestats"
)

// CompareJobsUsingKSTest takes a JobComparisonData object, compares left and
// right job samples of each metric inside it and fills in the comparison
// results in the metric's object after running a KS test on the two samples.
func CompareJobsUsingKSTest(jobComparisonData *util.JobComparisonData, significanceLevel, minMetricAvgForCompare float64) {
	jobComparisonData.ComputeStatsForMetricSamples()
	for _, metricData := range jobComparisonData.Data {
		leftSampleCount := len(metricData.LeftJobSample)
		rightSampleCount := len(metricData.RightJobSample)
		metricData.Matched = false
		var pValue float64
		if leftSampleCount == 0 || rightSampleCount == 0 {
			pValue = math.NaN()
			metricData.Matched = true
		} else {
			pValue = onlinestats.KS(metricData.LeftJobSample, metricData.RightJobSample)
			if pValue >= significanceLevel {
				metricData.Matched = true
			}
			if metricData.AvgL < minMetricAvgForCompare && metricData.AvgR < minMetricAvgForCompare {
				metricData.Matched = true
			}
		}
		metricData.Comments = fmt.Sprintf("Pvalue=%.4f\t\tN1=%v\tN2=%v", pValue, leftSampleCount, rightSampleCount)
	}
}
