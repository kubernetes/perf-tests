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

package prometheus

import (
	"testing"
)

func TestVerifySnapshotName(t *testing.T) {
	tests := []struct {
		name    string
		isValid bool
	}{
		{"disk-name", true},
		{"disk_name", false},
		{"disk12345", true},
		{"123242345", false},
	}

	for _, test := range tests {
		err := VerifySnapshotName(test.name)
		if test.isValid != (err == nil) {
			t.Errorf("Incorrect validation result of %s, got: %v, want: %v",
				test.name, (err == nil), test.isValid)
		}
	}
}
