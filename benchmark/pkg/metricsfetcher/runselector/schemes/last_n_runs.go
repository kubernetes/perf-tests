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
	"sort"

	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

// GetLastNJobRuns returns a list of run numbers of last 'n' completed runs of
// 'job'. If it cannot find n runs, returns as many runs as it could collect.
func GetLastNJobRuns(job string, n int, utils util.JobLogUtils) ([]int, error) {
	buildNumbers, err := utils.GetBuildNumbersForJob(job)
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.IntSlice(buildNumbers)))

	var runs []int
	for index := 0; index < len(buildNumbers) && len(runs) < n; index++ {
		buildNumber := buildNumbers[index]
		if _, err := utils.GetJobRunFinishedStatus(job, buildNumber); err == nil {
			runs = append(runs, buildNumber)
		}
	}
	return runs, nil
}
