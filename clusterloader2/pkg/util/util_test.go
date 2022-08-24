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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type test struct {
	Field1 string
	Field2 int
	*ObjectSelector
}

func TestToStruct(t *testing.T) {
	tests := []struct {
		name    string
		dict    map[string]interface{}
		in      interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name: "basic",
			dict: map[string]interface{}{
				"field1": "string1",
				"field2": 1234,
			},
			in: &test{},
			want: &test{
				Field1: "string1",
				Field2: 1234,
			},
		},
		{
			name: "preserves default values",
			dict: map[string]interface{}{
				"field2": 1234,
			},
			in: &test{
				Field1: "default value",
			},
			want: &test{
				Field1: "default value",
				Field2: 1234,
			},
		},
		{
			name: "With embed selector (WaitForControlledPodsRunning case)",
			dict: map[string]interface{}{
				"field1":        "string1",
				"namespace":     "namespace-1",
				"fieldSelector": "spec.nodeName=abcd",
				"labelSelector": "group = load",
			},
			in: &test{},
			want: &test{
				Field1: "string1",
				ObjectSelector: &ObjectSelector{
					Namespace:     "namespace-1",
					FieldSelector: "spec.nodeName=abcd",
					LabelSelector: "group = load",
				},
			},
		},
		{
			name: "type mismatch",
			dict: map[string]interface{}{
				"field1": 1234, // should be string
			},
			in:      &test{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ToStruct(tt.dict, tt.in); (err != nil) != tt.wantErr {
				t.Errorf("ToStruct() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assert.Equal(t, tt.want, tt.in)
			}
		})
	}
}
