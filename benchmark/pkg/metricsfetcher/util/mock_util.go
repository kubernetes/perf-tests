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
	"fmt"
)

// MockJobLogUtils mocks out all moving parts of JobLogUtils, like use of
// network or reading of files.
type MockJobLogUtils struct {
	JobLogUtils
	MockBuildNumbers     []int
	MockStartTimestamps  map[int]uint64
	MockFinishedStatuses map[int]bool
	MockBuildLogContents map[int][]byte
}

// GetLatestBuildNumberForJob returns latest build number for the job.
func (utils MockJobLogUtils) GetLatestBuildNumberForJob(job string) (int, error) {
	length := len(utils.MockBuildNumbers)
	if length == 0 {
		return 0, fmt.Errorf("Array of mock build numbers is empty")
	}
	return utils.MockBuildNumbers[length-1], nil
}

// GetJobRunStartTimestamp returns start timestamp for the job run.
func (utils MockJobLogUtils) GetJobRunStartTimestamp(job string, run int) (uint64, error) {
	value, ok := utils.MockStartTimestamps[run]
	if !ok {
		return 0, fmt.Errorf("Run number %v not a key in the mock start timestamps map", run)
	}
	return value, nil
}

// GetJobRunFinishedStatus returns the finished status (true/false) for the job run.
func (utils MockJobLogUtils) GetJobRunFinishedStatus(job string, run int) (bool, error) {
	value, ok := utils.MockFinishedStatuses[run]
	if !ok {
		return false, fmt.Errorf("Run number %v not a key in the mock finished statuses map", run)
	}
	return value, nil
}

// GetJobRunBuildLogContents returns the contents of the build log file for the job run.
func (utils MockJobLogUtils) GetJobRunBuildLogContents(job string, run int) ([]byte, error) {
	value, ok := utils.MockBuildLogContents[run]
	if !ok {
		return nil, fmt.Errorf("Run number %v not a key in the mock buildlog contents map", run)
	}
	return value, nil
}
