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

package config

import (
	"testing"
)

func TestTemplateRandData(t *testing.T) {
	tests := []struct {
		length int
	}{
		{
			length: 1,
		},
		{
			length: 100,
		},
	}

	for _, tt := range tests {
		data := randData(tt.length)
		if tt.length != len(data) {
			t.Errorf("wanted: %d, got: %d", tt.length, len(data))
		}
	}
}

func TestTemplateRandDataWithSeed(t *testing.T) {
	tests := []struct {
		length   int
		seed     string
		wantData string
	}{
		{
			length:   0,
			seed:     "test",
			wantData: "",
		},
		{
			length:   1,
			seed:     "test-1",
			wantData: "n",
		},
		{
			length:   1,
			seed:     "test-2",
			wantData: "P",
		},
		{
			length:   100,
			seed:     "test",
			wantData: "HlXm60Fjrvvp0pE3RK7lDhhhuQZpI6ECS18Gp0qqwFlOKjlZH4rFS3pk9pBkVRLzyqIWUv1Omfnmj7djTRjxL4bnp2kG7O5xD2Rf",
		},
	}

	for i, tt := range tests {
		data := randDataWithSeed(tt.length, tt.seed)

		if tt.wantData != data {
			t.Errorf("%d: wanted: %q, got: %q", i, tt.wantData, data)
		}
	}
}
