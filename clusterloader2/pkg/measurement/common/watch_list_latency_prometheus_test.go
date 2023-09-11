/*
Copyright 2023 The Kubernetes Authors.

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

package common

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common/executors"
)

func TestWatchListLatencyGather(t *testing.T) {
	scenarios := []struct {
		name          string
		inputFileName string

		duration time.Duration
	}{
		{
			name:          "smoke test: make sure the output matches the static golden file",
			inputFileName: "sample.yaml",
			duration:      10 * time.Minute,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			inputFilePath := fmt.Sprintf("testdata/watch_list_latency_prometheus/%s", scenario.inputFileName)
			executor, err := executors.NewPromqlExecutor(inputFilePath)
			if err != nil {
				t.Fatalf("failed to create PromQL executor: %v", err)
			}
			defer executor.Close()

			emptyConfig := &measurement.Config{Params: map[string]interface{}{}}
			target := &watchListLatencyGatherer{}
			start := time.Unix(0, 0).UTC()
			end := start.Add(scenario.duration)
			output, err := target.Gather(executor, start, end, emptyConfig)
			if err != nil {
				t.Fatal(err)
			}
			if len(output) != 1 {
				t.Fatalf("expected only one summary, got: %d", len(output))
			}

			rawGoldenFile, err := os.ReadFile(inputFilePath + ".golden")
			if err != nil {
				t.Fatalf("unable to read the golden file, err: %v", err)
			}
			if diff := cmp.Diff(string(rawGoldenFile), output[0].SummaryContent()); diff != "" {
				t.Errorf("unexpected output (-want +got):\n%s", diff)
			}
			// for simplicity, you can uncomment the following line to
			// generate a new golden file for a failed test case.
			//
			//os.WriteFile(inputFilePath+".golden", []byte(output[0].SummaryContent()), 0644)
		})
	}
}
