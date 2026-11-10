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
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

var podProducingKinds = map[string]bool{
	"Pod":                   true,
	"SandboxTemplate":       true,
	"CronJob":               true,
	"Deployment":            true,
	"StatefulSet":           true,
	"DaemonSet":             true,
	"Job":                   true,
	"ReplicationController": true,
	"ReplicaSet":            true,
}

var excludedSchedulerNameTemplates = map[string]string{
	"testing/list/pod.yaml":                                                                 "intentional non-existent scheduler",
	"testing/watch-list/pod.yaml":                                                           "intentional non-existent scheduler",
	"testing/write-throughput/modules/initial-pods/pod.yaml":                                "intentional non-existent scheduler",
	"testing/etcd/modules/operator/etcd-operator.yaml":                                      "test infra operator deployment",
	"testing/density/scheduler/custom-scheduler-metrics/deployment-custom-schd-sample.yaml": "sample manifest",
}

var topLevelKindPattern = regexp.MustCompile(`(?m)^kind:[ \t]*([^\s#]+)`)

func getSchedulerName(obj *unstructured.Unstructured) (string, bool, error) {
	if obj == nil {
		return "", false, nil
	}
	switch obj.GetKind() {
	case "Pod":
		return unstructured.NestedString(obj.Object, "spec", "schedulerName")
	case "SandboxTemplate":
		return unstructured.NestedString(obj.Object, "spec", "podTemplate", "spec", "schedulerName")
	case "CronJob":
		return unstructured.NestedString(obj.Object, "spec", "jobTemplate", "spec", "template", "spec", "schedulerName")
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "ReplicationController", "ReplicaSet":
		return unstructured.NestedString(obj.Object, "spec", "template", "spec", "schedulerName")
	default:
		return "", false, nil
	}
}

func discoverSchedulerNameTemplates(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	for excludedPath := range excludedSchedulerNameTemplates {
		if _, err := fs.Stat(fsys, excludedPath); err != nil {
			t.Fatalf("excluded template %s does not exist on disk: %v", excludedPath, err)
		}
	}

	var templates []string
	err := fs.WalkDir(fsys, "testing", func(relPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// fs.WalkDir marks symlinks with ModeSymlink rather than regular files; resolve via Stat.
		if d.Type()&fs.ModeSymlink != 0 {
			info, err := fs.Stat(fsys, relPath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
		}
		if path.Ext(relPath) != ".yaml" {
			return nil
		}
		data, err := fs.ReadFile(fsys, relPath)
		if err != nil {
			return err
		}
		for _, match := range topLevelKindPattern.FindAllSubmatch(data, -1) {
			kind := strings.Trim(string(match[1]), `"'`)
			if podProducingKinds[kind] {
				if _, excluded := excludedSchedulerNameTemplates[relPath]; !excluded {
					templates = append(templates, relPath)
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk testing templates: %v", err)
	}
	sort.Strings(templates)
	return templates
}

func defaultMapping() map[string]interface{} {
	return map[string]interface{}{
		"ApiVersion":             "v1",
		"BaseName":               "test-base",
		"CompletionReplicas":     1,
		"Completions":            1,
		"ContentType":            "application/json",
		"Count":                  1,
		"CPUMilli":               100,
		"CpuRequest":             "10m",
		"Duration":               "1m",
		"EnableWatchList":        false,
		"EnableWatchListFeature": false,
		"Group":                  "test-group",
		"Image":                  "registry.k8s.io/pause:3.9",
		"ImageRegistry":          "registry.k8s.io",
		"Index":                  0,
		"Inflight":               1,
		"Memory":                 "20Mi",
		"MemoryRequest":          "20Mi",
		"Mode":                   "Indexed",
		"Name":                   "test-obj",
		"Namespace":              "test-ns",
		"Nodes":                  1,
		"NumReplicas":            1,
		"PodCount":               1,
		"Provisioner":            "test-provisioner",
		"Qps":                    1,
		"QPS":                    1,
		"QpsPerWorker":           float64(1),
		"Replicas":               1,
		"ReplicasMax":            1,
		"ReplicasMin":            1,
		"Resource":               "pods",
		"Sleep":                  "1s",
		"StorageClass":           "standard",
		"SvcName":                "test-svc",
		"Tokens":                 1,
		"Uri":                    "/api/v1/pods",
		"VolSize":                "1Gi",
		"VolumesPerPod":          1,
	}
}

func TestGetSchedulerName(t *testing.T) {
	tests := []struct {
		name      string
		obj       *unstructured.Unstructured
		wantName  string
		wantFound bool
		wantErr   bool
	}{
		{
			name: "Pod with schedulerName",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Pod",
					"spec": map[string]interface{}{"schedulerName": "custom"},
				},
			},
			wantName:  "custom",
			wantFound: true,
		},
		{
			name: "Pod without schedulerName",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Pod",
					"spec": map[string]interface{}{},
				},
			},
		},
		{
			name: "SandboxTemplate with schedulerName",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "SandboxTemplate",
					"spec": map[string]interface{}{
						"podTemplate": map[string]interface{}{
							"spec": map[string]interface{}{"schedulerName": "custom"},
						},
					},
				},
			},
			wantName:  "custom",
			wantFound: true,
		},
		{
			name: "CronJob with schedulerName",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "CronJob",
					"spec": map[string]interface{}{
						"jobTemplate": map[string]interface{}{
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{"schedulerName": "custom"},
								},
							},
						},
					},
				},
			},
			wantName:  "custom",
			wantFound: true,
		},
		{
			name: "Deployment with schedulerName",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Deployment",
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{"schedulerName": "default-scheduler"},
						},
					},
				},
			},
			wantName:  "default-scheduler",
			wantFound: true,
		},
		{
			name: "non-pod kind ignored",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{"kind": "Service"},
			},
		},
		{
			name:    "non-map spec returns error",
			obj:     &unstructured.Unstructured{Object: map[string]interface{}{"kind": "Pod", "spec": "invalid"}},
			wantErr: true,
		},
		{
			name: "non-string schedulerName returns error",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Pod",
					"spec": map[string]interface{}{"schedulerName": int64(123)},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotFound, err := getSchedulerName(tc.obj)
			if (err != nil) != tc.wantErr {
				t.Fatalf("getSchedulerName() error = %v, wantErr %v", err, tc.wantErr)
			}
			if gotFound != tc.wantFound || gotName != tc.wantName {
				t.Errorf("getSchedulerName() = (%q, %v), want (%q, %v)", gotName, gotFound, tc.wantName, tc.wantFound)
			}
		})
	}
}

func TestTemplateSchedulerNameSupport(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "../.."))
	fsys := os.DirFS(repoRoot)
	tp := NewTemplateProvider(fsys)

	templates := discoverSchedulerNameTemplates(t, fsys)
	if len(templates) < 40 {
		t.Fatalf("expected at least 40 workload templates, discovered %d", len(templates))
	}

	schedulerCases := []struct {
		name          string
		schedulerName string
		wantScheduler string
	}{
		{name: "unset", wantScheduler: "default-scheduler"},
		{name: "custom", schedulerName: "custom-test-scheduler", wantScheduler: "custom-test-scheduler"},
	}

	for _, relPath := range templates {
		t.Run(relPath, func(t *testing.T) {
			for _, tc := range schedulerCases {
				t.Run(tc.name, func(t *testing.T) {
					mapping := defaultMapping()
					if tc.schedulerName != "" {
						mapping["CL2_SCHEDULER_NAME"] = tc.schedulerName
					}
					rendered, err := tp.getMappedTemplate(relPath, mapping)
					if err != nil {
						t.Fatalf("getMappedTemplate failed for %s: %v", relPath, err)
					}
					if bytes.Contains(rendered, []byte("<no value>")) {
						t.Errorf("rendered template %s contains <no value> placeholder:\n%s", relPath, string(rendered))
					}
					objs, err := tp.TemplateToObjects(relPath, mapping)
					if err != nil {
						t.Fatalf("TemplateToObjects failed for %s: %v", relPath, err)
					}
					if len(objs) == 0 {
						t.Fatalf("TemplateToObjects returned 0 objects for %s", relPath)
					}
					podObjCount := 0
					for _, obj := range objs {
						if !podProducingKinds[obj.GetKind()] {
							continue
						}
						podObjCount++
						sName, exists, err := getSchedulerName(obj)
						if err != nil {
							t.Fatalf("error getting schedulerName in %s: %v", relPath, err)
						}
						if !exists || sName != tc.wantScheduler {
							t.Errorf("expected schedulerName %q, got exists=%v name=%q in %s", tc.wantScheduler, exists, sName, relPath)
						}
					}
					if podObjCount == 0 {
						t.Errorf("expected at least 1 pod-producing object in %s", relPath)
					}
				})
			}
		})
	}

	const podStartupLatencyModule = "testing/load/modules/pod-startup-latency.yaml"
	t.Run(podStartupLatencyModule, func(t *testing.T) {
		for _, tc := range schedulerCases {
			t.Run(tc.name, func(t *testing.T) {
				mapping := map[string]interface{}{
					"namespaces":            1,
					"minPodsInSmallCluster": 1,
					"image":                 "registry.k8s.io/pause:3.9",
				}
				if tc.schedulerName != "" {
					mapping["CL2_SCHEDULER_NAME"] = tc.schedulerName
				}
				rendered, err := tp.getMappedTemplate(podStartupLatencyModule, mapping)
				if err != nil {
					t.Fatalf("getMappedTemplate failed: %v", err)
				}
				if bytes.Contains(rendered, []byte("<no value>")) {
					t.Errorf("rendered module contains <no value> placeholder:\n%s", string(rendered))
				}
				var mod api.Module
				if err := tp.TemplateInto(podStartupLatencyModule, mapping, &mod); err != nil {
					t.Fatalf("TemplateInto failed: %v", err)
				}
				foundGather := false
				for _, step := range mod.Steps {
					for _, m := range step.Measurements {
						if m.Method == "PodStartupLatency" && m.Params["action"] == "gather" {
							foundGather = true
							if got := m.Params["schedulerName"]; got != tc.wantScheduler {
								t.Errorf("PodStartupLatency gather schedulerName = %v, want %q", got, tc.wantScheduler)
							}
						}
					}
				}
				if !foundGather {
					t.Fatalf("PodStartupLatency gather measurement not found in %s", podStartupLatencyModule)
				}
			})
		}
	})
}
