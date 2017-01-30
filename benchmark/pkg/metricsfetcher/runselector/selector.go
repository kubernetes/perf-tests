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

package runselector

import (
	"fmt"

	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/runselector/schemes"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

// Allowed run-selection schemes.
const (
	LastNRuns  = "last-n-runs"
	LastNHours = "last-n-hours"
)

// GetJobRunsUsingScheme is a wrapper function for various run selection schemes.
func GetJobRunsUsingScheme(job string, scheme string, nValue int, utils util.JobLogUtils) ([]int, error) {
	switch scheme {
	case LastNRuns:
		return schemes.GetLastNJobRuns(job, nValue, utils)
	case LastNHours:
		return schemes.GetJobRunsFromLastNHours(job, nValue, utils)
	default:
		return nil, fmt.Errorf("Unknown run selection scheme '%v'", scheme)
	}
}
