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
	"time"

	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

const (
	lastNhoursJob = "testNhours"
	numHours      = 2
)

func TestGetJobRunsFromLastNHours(t *testing.T) {
	currentTime := uint64(time.Now().Unix())
	utils := util.MockJobLogUtils{
		MockBuildNumbers: []int{1, 3, 5, 6, 7, 9},
		MockStartTimestamps: map[int]uint64{
			1: (currentTime - 8000),
			3: (currentTime - 7000),
			5: (currentTime - 6000),
			7: (currentTime - 5000),
			9: (currentTime - 4000),
		},
		MockFinishedStatuses: map[int]bool{
			1: true,
			3: true,
			6: true,
			7: false,
			9: true,
		},
	}
	expected := []int{9, 7, 3}

	runs, err := GetJobRunsFromLastNHours(lastNhoursJob, numHours, utils)
	if err != nil {
		t.Errorf("Error obtaining runs from last %v runs for job '%v': %v", numHours, lastNhoursJob, err)
	}

	if !reflect.DeepEqual(runs, expected) {
		t.Errorf("Expected runs %v but got %v for job '%v' using GetJobRunsFromLastNHours", expected, runs, lastNhoursJob)
	}
}
