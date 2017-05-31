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

package comparer

import (
	"fmt"

	"k8s.io/perf-tests/benchmark/pkg/comparer/schemes"
	"k8s.io/perf-tests/benchmark/pkg/util"
)

// Allowed comparison schemes.
const (
	AvgTest = "Avg-Test"
	KSTest  = "KS-Test"
)

// CompareJobsUsingScheme is a wrapper function for various comparison schemes.
func CompareJobsUsingScheme(jobComparisonData *util.JobComparisonData, scheme string, matchThreshold, minMetricAvgForCompare float64) error {
	switch scheme {
	case AvgTest:
		// matchThreshold is interpreted as the bound for ratio of left and right sample avgs for this test.
		schemes.CompareJobsUsingAvgTest(jobComparisonData, matchThreshold, minMetricAvgForCompare)
		return nil
	case KSTest:
		// matchThreshold is interpreted as the allowed significance value for this test.
		schemes.CompareJobsUsingKSTest(jobComparisonData, matchThreshold, minMetricAvgForCompare)
		return nil
	default:
		return fmt.Errorf("Unknown comparison scheme '%v'", scheme)
	}
}
