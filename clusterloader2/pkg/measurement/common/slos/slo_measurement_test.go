/*
Copyright 2020 The Kubernetes Authors.

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

package slos

import (
	"testing"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func Test_getMeasurementConfig(t *testing.T) {
	config := &measurement.Config{
		Params: map[string]interface{}{
			"threshold": 100,
			"latency":   "5s",
			"overrides": map[string]interface{}{
				"SchedulingThroughput": map[string]interface{}{
					"threshold": 200,
				},
				"PodStartupLatency": map[string]interface{}{
					"threshold": 5,
					"latency":   "10s",
				},
			},
		},
	}

	measurementConfig, _ := getMeasurementConfig(config, "SchedulingThroughput")
	params := measurementConfig.Params
	if result := params["threshold"]; result != 200 {
		t.Errorf("want %v, got %v", 200, result)
	}
	if result := params["latency"]; result != "5s" {
		t.Errorf("want %v, got %v", 200, result)
	}

	measurementConfig, _ = getMeasurementConfig(config, "PodStartupLatency")
	params = measurementConfig.Params
	if result := params["threshold"]; result != 5 {
		t.Errorf("want %v, got %v", 5, result)
	}
	if result := params["latency"]; result != "10s" {
		t.Errorf("want %v, got %v", "10s", result)
	}

	measurementConfig, _ = getMeasurementConfig(config, "OtherMeasurement")
	params = measurementConfig.Params
	if result := params["threshold"]; result != 100 {
		t.Errorf("want %v, got %v", 100, result)
	}
	if result := params["latency"]; result != "5s" {
		t.Errorf("want %v, got %v", "5s", result)
	}
}

func Test_getMeasurementConfig_incorect_overrides(t *testing.T) {
	config := &measurement.Config{
		Params: map[string]interface{}{
			"threshold": 100,
			"latency":   "5s",
			"overrides": map[string]interface{}{
				"SchedulingThroughput": []string{"threshold", "200"},
			},
		},
	}

	_, err := getMeasurementConfig(config, "SchedulingThroughput")
	if err == nil {
		t.Error("Did not return error for incorrect config")
	}
}
