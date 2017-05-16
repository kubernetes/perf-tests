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
	"reflect"
	"testing"

	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

const (
	lastNrunsJob = "testNruns"
	numRuns      = 3
)

func TestGetLastNJobRuns(t *testing.T) {
	utils := util.MockJobLogUtils{
		MockBuildNumbers:    []int{1, 3, 5, 6, 7, 9},
		MockStartTimestamps: nil,
		MockFinishedStatuses: map[int]bool{
			1: true,
			3: true,
			5: true,
			7: false,
			9: true,
		},
	}
	expected := []int{9, 7, 5}

	runs, err := GetLastNJobRuns(lastNrunsJob, numRuns, utils)
	if err != nil {
		t.Errorf("Error obtaining last %v runs for job '%v': %v", numRuns, lastNrunsJob, err)
	}

	if !reflect.DeepEqual(runs, expected) {
		t.Errorf("Expected runs %v but got %v for job '%v' using GetLastNJobRuns", expected, runs, lastNrunsJob)
	}
}
