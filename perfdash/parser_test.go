/*
Copyright 2016 The Kubernetes Authors.

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

package main

import (
	"reflect"
	"testing"

	"k8s.io/kubernetes/test/e2e/perftype"
)

func Test_parseSystemPodMetrics(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		buildNumber int
		testResult  *BuildData
		want        *BuildData
	}{
		{
			name:        "same-container-in-two-pods",
			buildNumber: 123,
			data:        sameContainerInTwoPodsSummary(),
			testResult:  &BuildData{Job: "", Version: "", Builds: map[string][]perftype.DataItem{}},
			want: &BuildData{Job: "", Version: "", Builds: map[string][]perftype.DataItem{
				"123": {
					perftype.DataItem{
						Data: map[string]float64{
							"c1": 2,
							"c2": 1,
						},
						Labels: map[string]string{
							"RestartCount": "RestartCount",
						},
						Unit: "",
					},
				},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseSystemPodMetrics(tt.data, tt.buildNumber, tt.testResult)
			if !reflect.DeepEqual(*tt.testResult, *tt.want) {
				t.Errorf("want %v, got %v", *tt.want, *tt.testResult)
			}
		})
	}
}

func sameContainerInTwoPodsSummary() []byte {
	json := `{
		"pods": [
			{
				"name": "p1",
				"containers": [
					{
						"name": "c1",
						"restartCount": 2
					},
					{
						"name": "c2",
						"restartCount": 1
					}
				]
			},
			{
				"name": "p2",
				"containers": [
					{
						"name": "c1",
						"restartCount": 0
					}
				]
			}
		]
	}`
	return []byte(json)
}
