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
	"time"

	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

// GetJobRunsFromLastNHours returns a list of run numbers of 'job' from last 'n' hours,
// that've finished. If it cannot find n runs, returns as many runs as it could collect.
// Note: We check if start time of the run is not earlier than 'n' hours from now.
func GetJobRunsFromLastNHours(job string, n int, utils util.JobLogUtils) ([]int, error) {
	latestRunNumber, err := utils.GetLatestBuildNumberForJob(job)
	if err != nil {
		return nil, err
	}

	var runs []int
	currentTime := uint64(time.Now().Unix())
	for runNumber := latestRunNumber; runNumber > 0; runNumber-- {
		if startTimestamp, err := utils.GetJobRunStartTimestamp(job, runNumber); err == nil {
			if (currentTime - startTimestamp) < uint64(3600*n) {
				if _, err := utils.GetJobRunFinishedStatus(job, runNumber); err == nil {
					runs = append(runs, runNumber)
				}
			} else {
				break
			}
		}
	}
	return runs, nil
}
