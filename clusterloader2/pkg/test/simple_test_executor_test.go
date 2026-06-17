/*
Copyright 2026 The Kubernetes Authors.

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

package test

import (
	"strings"
	"testing"
	"testing/fstest"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/chaos"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/dependency"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/tuningset"
)

type mockContext struct {
	templateProvider *config.TemplateProvider
}

func (m *mockContext) GetClusterLoaderConfig() *config.ClusterLoaderConfig { return nil }
func (m *mockContext) GetClusterFramework() *framework.Framework           { return nil }
func (m *mockContext) GetPrometheusFramework() *framework.Framework        { return nil }
func (m *mockContext) GetTestReporter() Reporter                           { return nil }
func (m *mockContext) GetState() *state.State                              { return nil }
func (m *mockContext) GetTemplateMappingCopy() map[string]interface{} {
	return map[string]interface{}{}
}
func (m *mockContext) GetTemplateProvider() *config.TemplateProvider {
	return m.templateProvider
}
func (m *mockContext) GetFactory() tuningset.Factory            { return nil }
func (m *mockContext) GetManager() measurement.Manager          { return nil }
func (m *mockContext) GetDependencyManager() dependency.Manager { return nil }
func (m *mockContext) GetChaosMonkey() *chaos.Monkey            { return nil }
func (m *mockContext) GetTestScenario() *api.TestScenario       { return nil }
func (m *mockContext) GetTestConfig() *api.Config               { return nil }
func (m *mockContext) SetTestConfig(*api.Config)                {}

func TestExecuteObjectDuplicateGVK(t *testing.T) {
	fsys := fstest.MapFS{
		"duplicate_gvk.yaml": &fstest.MapFile{
			Data: []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: pod-1
---
apiVersion: v1
kind: Pod
metadata:
  name: pod-2
`),
		},
	}

	tp := config.NewTemplateProvider(fsys)
	ctx := &mockContext{templateProvider: tp}
	ste := &simpleExecutor{}

	obj := &api.Object{
		Basename:           "test-pod",
		ObjectTemplatePath: "duplicate_gvk.yaml",
	}

	errList := ste.ExecuteObject(ctx, obj, "default", 0, createObject)
	if errList == nil || errList.IsEmpty() {
		t.Fatal("expected duplicate GVK error, got no errors")
	}

	expectedErr := `duplicate GroupVersionKind "/v1, Kind=Pod" in multi-document template duplicate_gvk.yaml`
	if !strings.Contains(errList.Error(), expectedErr) {
		t.Fatalf("expected error containing %q, got: %s", expectedErr, errList.Error())
	}
}
