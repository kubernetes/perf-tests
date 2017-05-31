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
)

// CompareJobsUsingAvgTest takes a JobComparisonData object, compares left
// and right jobs for each metric inside it and fills in the comparison
// results in the metric's object after checking ratio of the averages
// of its left and right samples is within the allowed ratio lower bound
// and upper bound (which is the inverse of lower bound).
func CompareJobsUsingAvgTest(jobComparisonData *util.JobComparisonData, allowedRatioLowerBound, minMetricAvgForCompare float64) {
	jobComparisonData.ComputeStatsForMetricSamples()
	for _, metricData := range jobComparisonData.Data {
		leftSampleCount := len(metricData.LeftJobSample)
		rightSampleCount := len(metricData.RightJobSample)
		metricData.Matched = false
		if leftSampleCount == 0 || rightSampleCount == 0 {
			metricData.AvgRatio = math.NaN()
			metricData.Matched = true
		} else {
			metricData.AvgRatio = metricData.AvgL / metricData.AvgR
			if allowedRatioLowerBound <= metricData.AvgRatio && metricData.AvgRatio <= 1/allowedRatioLowerBound {
				metricData.Matched = true
			}
			if metricData.AvgL < minMetricAvgForCompare && metricData.AvgR < minMetricAvgForCompare {
				metricData.Matched = true
			}
		}
		metricData.Comments = fmt.Sprintf("AvgL/R=%.2f\tAvgL(ms)=%.2f\tAvgR(ms)=%.2f\tN1=%v\tN2=%v", metricData.AvgRatio, metricData.AvgL, metricData.AvgR, leftSampleCount, rightSampleCount)
	}
}
