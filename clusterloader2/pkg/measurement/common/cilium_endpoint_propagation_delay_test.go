/*
Copyright 2022 The Kubernetes Authors.

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common/executors"
)

func TestCiliumEndpointPropagationDelayMeasurement(t *testing.T) {
	cases := []struct {
		name               string
		config             *measurement.Config
		hasError           bool
		testSeriesFile     string
		testSeriesDuration time.Duration
	}{
		{
			name:               "default_slo_pass",
			hasError:           false,
			testSeriesFile:     "default_slo_pass.yaml",
			testSeriesDuration: 100 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
		},
		{
			name:               "default_slo_fail",
			hasError:           true,
			testSeriesFile:     "default_slo_fail.yaml",
			testSeriesDuration: 100 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
		},
		{
			name:               "custom_slo_pass",
			hasError:           false,
			testSeriesFile:     "default_slo_pass.yaml",
			testSeriesDuration: 100 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{
					"bucketSLO":     float64(600),
					"percentileSLO": float64(99),
				},
			},
		},
		{
			name:               "custom_slo_fail",
			hasError:           true,
			testSeriesFile:     "default_slo_fail.yaml",
			testSeriesDuration: 100 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{
					"bucketSLO":     float64(1),
					"percentileSLO": float64(99),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor, err := executors.NewPromqlExecutor(fmt.Sprintf("testdata/cilium_endpoint_propagation_delay/%s", tc.testSeriesFile))
			if err != nil {
				t.Fatalf("failed to create PromQL executor: %v", err)
			}
			defer executor.Close()
			gatherer := &cepPropagationDelayGatherer{}
			start := time.Unix(0, 0).UTC()
			end := start.Add(tc.testSeriesDuration)
			_, err = gatherer.Gather(executor, start, end, tc.config)
			if tc.hasError {
				assert.NotNil(t, err, "Wanted error, but got none")
			} else {
				assert.Nil(t, err, "Wanted no error, but got %v", err)
			}
		})
	}
}
