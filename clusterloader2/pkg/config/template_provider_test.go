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
	"os"
	"reflect"
	"testing"
	"testing/fstest"

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

func TestLoadCL2Envs(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		wantedMapping map[string]interface{}
	}{
		{
			name: "One CL2 env, one non-CL2 env",
			env: map[string]string{
				"CL2_MY_PARAM": "100",
				"NODE_SIZE":    "n1-standard-1",
			},
			wantedMapping: map[string]interface{}{
				"CL2_MY_PARAM": int64(100),
			},
		},
		{
			name: "Multiple CL2 envs",
			env: map[string]string{
				"CL2_MY_PARAM1": "100",
				"CL2_MY_PARAM2": "true",
				"CL2_MY_PARAM3": "99.99",
				"CL2_MY_PARAM4": "XXX",
				"CL2_MY_PARAM5": "1",
				"CL2_MY_PARAM6": "a=b",
			},
			wantedMapping: map[string]interface{}{
				"CL2_MY_PARAM1": int64(100),
				"CL2_MY_PARAM2": true,
				"CL2_MY_PARAM3": 99.99,
				"CL2_MY_PARAM4": "XXX",
				"CL2_MY_PARAM5": int64(1),
				"CL2_MY_PARAM6": "a=b",
			},
		},
		{
			name: "No CL2 envs",
			env: map[string]string{
				"NODE_SIZE": "n1-standard-1",
				"CLUSTER":   "my-cluster",
			},
			wantedMapping: map[string]interface{}{},
		},
		{
			name: "Env prefix is case sensitive",
			env: map[string]string{
				"cl2_my_param": "123",
			},
			wantedMapping: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			mapping, err := LoadCL2Envs()
			if err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(mapping, tt.wantedMapping) {
				t.Errorf("wanted: %v, got: %v", tt.wantedMapping, mapping)
			}
		})
	}
}

func TestMergeMappings(t *testing.T) {
	tests := []struct {
		name    string
		a       map[string]interface{}
		b       map[string]interface{}
		wantedA map[string]interface{}
		wantErr bool
	}{
		{
			name: "Different keys",
			a:    map[string]interface{}{"ENABLE_XXX": true},
			b:    map[string]interface{}{"CL2_PARAM1": 123},
			wantedA: map[string]interface{}{
				"ENABLE_XXX": true,
				"CL2_PARAM1": 123,
			},
		},
		{
			name: "Same keys, no conflict",
			a:    map[string]interface{}{"CL2_PARAM1": 100},
			b:    map[string]interface{}{"CL2_PARAM1": 100},
			wantedA: map[string]interface{}{
				"CL2_PARAM1": 100,
			},
		},
		{
			name:    "Same keys, conflict",
			a:       map[string]interface{}{"CL2_PARAM1": 100},
			b:       map[string]interface{}{"CL2_PARAM1": 105},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := MergeMappings(tt.a, tt.b); err != nil {
				if !tt.wantErr {
					t.Errorf("unexpceted MergeMappings() error: %v", err)
				}
				return
			}
			if !reflect.DeepEqual(tt.a, tt.wantedA) {
				t.Errorf("wanted: %v, got: %v", tt.wantedA, tt.a)
			}
		})
	}
}

func TestTemplateProvider(t *testing.T) {
	fsys := fstest.MapFS{
		"single.yaml": &fstest.MapFile{
			Data: []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: pod-1
`),
		},
		"multi.yaml": &fstest.MapFile{
			Data: []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: pod-1
---
# Some comment
apiVersion: v1
kind: Service
metadata:
  name: svc-1
---
`),
		},
		"templated.yaml": &fstest.MapFile{
			Data: []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: {{.Name}}-pod
---
apiVersion: v1
kind: Service
metadata:
  name: {{.Name}}-svc
`),
		},
	}

	tp := NewTemplateProvider(fsys)

	t.Run("RawToObjects single document", func(t *testing.T) {
		objs, err := tp.RawToObjects("single.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(objs) != 1 {
			t.Fatalf("expected 1 object, got %d", len(objs))
		}
		if objs[0].GetName() != "pod-1" {
			t.Errorf("expected name pod-1, got %s", objs[0].GetName())
		}
	})

	t.Run("RawToObjects multiple documents", func(t *testing.T) {
		objs, err := tp.RawToObjects("multi.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(objs) != 2 {
			t.Fatalf("expected 2 objects, got %d", len(objs))
		}
		if objs[0].GetName() != "pod-1" || objs[0].GetKind() != "Pod" {
			t.Errorf("expected pod-1 (Pod), got %s (%s)", objs[0].GetName(), objs[0].GetKind())
		}
		if objs[1].GetName() != "svc-1" || objs[1].GetKind() != "Service" {
			t.Errorf("expected svc-1 (Service), got %s (%s)", objs[1].GetName(), objs[1].GetKind())
		}
	})

	t.Run("TemplateToObjects multiple documents with mapping", func(t *testing.T) {
		mapping := map[string]interface{}{
			"Name": "test-app",
		}
		objs, err := tp.TemplateToObjects("templated.yaml", mapping)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(objs) != 2 {
			t.Fatalf("expected 2 objects, got %d", len(objs))
		}
		if objs[0].GetName() != "test-app-pod" || objs[0].GetKind() != "Pod" {
			t.Errorf("expected test-app-pod (Pod), got %s (%s)", objs[0].GetName(), objs[0].GetKind())
		}
		if objs[1].GetName() != "test-app-svc" || objs[1].GetKind() != "Service" {
			t.Errorf("expected test-app-svc (Service), got %s (%s)", objs[1].GetName(), objs[1].GetKind())
		}
	})
}
