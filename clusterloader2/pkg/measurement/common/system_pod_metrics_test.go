/*
Copyright 2019 The Kubernetes Authors.

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
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func Test_subtractInitialRestartCounts(t *testing.T) {
	tests := []struct {
		name        string
		metrics     *systemPodsMetrics
		initMetrics *systemPodsMetrics
		want        *systemPodsMetrics
	}{
		{
			name:        "same-pods-and-containers",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p1", "c1", 4),
			want:        generatePodMetrics("p1", "c1", 1),
		},
		{
			name:        "different-container-names",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p1", "c2", 4),
			want:        generatePodMetrics("p1", "c1", 5),
		},
		{
			name:        "different-pod-names",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p2", "c1", 4),
			want:        generatePodMetrics("p1", "c1", 5),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subtractInitialRestartCounts(tt.metrics, tt.initMetrics)
			if !reflect.DeepEqual(*tt.metrics, *tt.want) {
				t.Errorf("want %v, got %v", *tt.want, *tt.metrics)
			}
		})
	}
}

func Test_validateRestartCounts(t *testing.T) {
	tests := []struct {
		name    string
		metrics *systemPodsMetrics
		config  *measurement.Config
		wantErr bool
	}{
		{
			name:    "check-disabled",
			metrics: generatePodMetrics("p", "c", 1),
			config:  buildConfig(t, false, nil),
			wantErr: false,
		},
		{
			name:    "check-enabled-violation",
			metrics: generatePodMetrics("p", "c", 1),
			config:  buildConfig(t, true, nil),
			wantErr: true,
		},
		{
			name:    "check-enabled-ok",
			metrics: generatePodMetrics("p", "c", 0),
			config:  buildConfig(t, true, nil),
			wantErr: false,
		},
		{
			name:    "override-equal-to-actual-count",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"c": 3}),
			wantErr: false,
		},
		{
			name:    "override-default-used",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"default": 3}),
			wantErr: false,
		},
		{
			name:    "override-default-not-used",
			metrics: generatePodMetrics("p", "c", 3),
			config: buildConfig(t, true, map[string]int{
				"default": 5,
				"c":       0,
			}),
			wantErr: true,
		},
		{
			name:    "override-below-actual-count",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"c": 2}),
			wantErr: true,
		},
		{
			name:    "override-for-different-container",
			metrics: generatePodMetrics("p", "c1", 3),
			config:  buildConfig(t, true, map[string]int{"c2": 4}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overrides, err := getThresholdOverrides(tt.config)
			if err != nil {
				t.Fatalf("getThresholdOverrides() error = %v", err)
			}
			if err := validateRestartCounts(tt.metrics, tt.config, overrides); (err != nil) != tt.wantErr {
				t.Errorf("verifyViolations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func generatePodMetrics(podName string, contName string, restartCount int32) *systemPodsMetrics {
	return &systemPodsMetrics{
		Pods: []podMetrics{
			{
				Name: podName,
				Containers: []containerMetrics{
					{
						Name:         contName,
						RestartCount: restartCount,
					},
				}},
		},
	}
}

func buildConfig(t *testing.T, checkEnabled bool, thresholdOverrides map[string]int) *measurement.Config {
	serializedOverrides, err := yaml.Marshal(thresholdOverrides)
	if err != nil {
		t.Fatal(err)
	}
	return &measurement.Config{
		Params: map[string]interface{}{
			"enableRestartCountCheck":        checkEnabled,
			"restartCountThresholdOverrides": string(serializedOverrides),
		},
	}
}
