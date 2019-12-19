/*
Copyright 2018 The Kubernetes Authors.

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

	"k8s.io/perf-tests/clusterloader2/api"
)

func TestValidateTestSuite(t *testing.T) {
	tests := []struct {
		name    string
		suite   api.TestSuite
		wantErr bool
	}{
		{
			name:    "empty-suite",
			suite:   api.TestSuite{},
			wantErr: false,
		},
		{
			name: "valid-id",
			suite: api.TestSuite{
				api.TestScenario{
					Identifier:    "some-id",
					ConfigPath:    "",
					OverridePaths: []string{},
				},
			},
			wantErr: false,
		},
		{
			name: "id-with-underscore",
			suite: api.TestSuite{
				api.TestScenario{
					Identifier:    "some_id",
					ConfigPath:    "",
					OverridePaths: []string{},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTestSuite(tt.suite); (err != nil) != tt.wantErr {
				t.Errorf("validateTestSuite() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
