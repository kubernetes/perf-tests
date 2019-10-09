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
