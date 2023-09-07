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

package util

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestWaitForGenericK8sObjects(t *testing.T) {

	tests := []struct {
		name            string
		timeout         time.Duration
		options         *WaitForGenericK8sObjectsOptions
		existingObjects []exampleObject
		wantErr         bool
	}{
		{
			name:    "one successful object",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kuberentes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{},
				MinDesiredObjectCount: 1,
				MaxFailedObjectCount:  0,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObject("test-1", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
			},
		},
		{
			name:    "one failed object",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kuberentes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{"Failed=True"},
				MinDesiredObjectCount: 1,
				MaxFailedObjectCount:  0,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObject("test-1", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "False",
					},
					map[string]interface{}{
						"type":   "Failed",
						"status": "True",
					},
				}),
			},
			wantErr: true,
		},
		{
			name:    "one failed object, but one is acceptable",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kuberentes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{"Failed=True"},
				MinDesiredObjectCount: 3,
				MaxFailedObjectCount:  1,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObject("test-1", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "False",
					},
					map[string]interface{}{
						"type":   "Failed",
						"status": "True",
					},
				}),
				newExampleObject("test-2", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
				newExampleObject("test-3", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
			},
		},
		{
			name:    "timeout due not enough objects",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kuberentes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{"Failed=True"},
				MinDesiredObjectCount: 3,
				MaxFailedObjectCount:  1,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObject("test-1", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "False",
					},
					map[string]interface{}{
						"type":   "Failed",
						"status": "True",
					},
				}),
				newExampleObject("test-2", "namespace-1", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				tt.options.GroupVersionResource: "ConditionsList",
			})
			for _, o := range tt.existingObjects {
				c := dynamicClient.Resource(tt.options.GroupVersionResource).Namespace(o.Namespace)
				if _, err := c.Create(ctx, o.Unstructured, metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create an existing object %v, got error: %v", o, err)
				}
			}

			if err := WaitForGenericK8sObjects(ctx, dynamicClient, tt.options); (err != nil) != tt.wantErr {
				t.Errorf("WaitForGenericK8sObjects() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type exampleObject struct {
	Namespace    string
	Unstructured *unstructured.Unstructured
}

func newExampleObject(name, namespace string, conditions []interface{}) exampleObject {
	return exampleObject{
		Namespace: namespace,
		Unstructured: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"status": map[string]interface{}{
					"conditions": conditions,
				},
			},
		},
	}
}
