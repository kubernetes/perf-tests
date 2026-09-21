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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

type fakeGenericLister struct {
	objects []runtime.Object
}

func (f *fakeGenericLister) List(selector labels.Selector) (ret []runtime.Object, err error) {
	for _, obj := range f.objects {
		if pod, ok := obj.(*corev1.Pod); ok {
			if selector.Matches(labels.Set(pod.Labels)) {
				ret = append(ret, obj)
			}
		}
	}
	return ret, nil
}

func (f *fakeGenericLister) Get(name string) (runtime.Object, error) {
	return nil, nil
}

func (f *fakeGenericLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return nil
}

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
		{
			name:    "successful objects across all namespaces with AllNamespaces true",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kuberentes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					AllNamespaces: true,
				},
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{},
				MinDesiredObjectCount: 2,
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
				newExampleObject("test-2", "namespace-2", []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
			},
		},
		{
			name:    "successful object filtered by label selector",
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
				LabelSelector:         labels.SelectorFromSet(labels.Set{"app": "worker"}),
				SuccessfulConditions:  []string{"Successful=True"},
				FailedConditions:      []string{},
				MinDesiredObjectCount: 1,
				MaxFailedObjectCount:  0,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObjectWithLabels("test-1", "namespace-1", map[string]string{"app": "worker"}, []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
				newExampleObjectWithLabels("test-2", "namespace-1", map[string]string{"app": "api"}, []interface{}{
					map[string]interface{}{
						"type":   "Successful",
						"status": "True",
					},
				}),
			},
		},
		{
			name:    "objects created with empty conditions (no conditions specified)",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "kubernetes.io",
					Version:  "v1alpha1",
					Resource: "Conditions",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				SuccessfulConditions:  []string{},
				FailedConditions:      []string{},
				MinDesiredObjectCount: 2,
				MaxFailedObjectCount:  0,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
			},
			existingObjects: []exampleObject{
				newExampleObject("test-1", "namespace-1", []interface{}{}),
				newExampleObject("test-2", "namespace-1", []interface{}{}),
			},
		},
		{
			name:    "successful pods (special case) filtered by label selector",
			timeout: 1 * time.Second,
			options: &WaitForGenericK8sObjectsOptions{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    "",
					Version:  "v1",
					Resource: "pods",
				},
				Namespaces: NamespacesRange{
					Prefix: "namespace",
					Min:    1,
					Max:    1,
				},
				LabelSelector:         labels.SelectorFromSet(labels.Set{"app": "worker"}),
				SuccessfulConditions:  []string{"Ready=True"},
				FailedConditions:      []string{},
				MinDesiredObjectCount: 1,
				MaxFailedObjectCount:  0,
				CallerName:            "test",
				WaitInterval:          100 * time.Millisecond,
				GenericLister: &fakeGenericLister{
					objects: []runtime.Object{
						&corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-1",
								Namespace: "namespace-1",
								Labels:    map[string]string{"app": "worker"},
							},
							Status: corev1.PodStatus{
								Conditions: []corev1.PodCondition{
									{Type: corev1.PodReady, Status: corev1.ConditionTrue},
								},
							},
						},
						&corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-2",
								Namespace: "namespace-1",
								Labels:    map[string]string{"app": "api"},
							},
							Status: corev1.PodStatus{
								Conditions: []corev1.PodCondition{
									{Type: corev1.PodReady, Status: corev1.ConditionTrue},
								},
							},
						},
					},
				},
			},
			existingObjects: nil,
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

			if tt.options.GenericLister == nil {
				tweakListOptions := func(listOptions *metav1.ListOptions) {
					if tt.options.LabelSelector != nil {
						listOptions.LabelSelector = tt.options.LabelSelector.String()
					}
				}
				informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 10*time.Second, metav1.NamespaceAll, tweakListOptions)
				tt.options.GenericLister = informerFactory.ForResource(tt.options.GroupVersionResource).Lister()
				informerFactory.Start(ctx.Done())
				informerFactory.WaitForCacheSync(ctx.Done())
			}

			if err := WaitForGenericK8sObjects(ctx, tt.options); (err != nil) != tt.wantErr {
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
	return newExampleObjectWithLabels(name, namespace, nil, conditions)
}

func newExampleObjectWithLabels(name, namespace string, objectLabels map[string]string, conditions []interface{}) exampleObject {
	labels := map[string]interface{}{}
	for k, v := range objectLabels {
		labels[k] = v
	}
	return exampleObject{
		Namespace: namespace,
		Unstructured: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
					"labels":    labels,
				},
				"status": map[string]interface{}{
					"conditions": conditions,
				},
			},
		},
	}
}

func TestNamespacesRange_AllNamespaces(t *testing.T) {
	nr := NamespacesRange{
		AllNamespaces: true,
	}
	if got := nr.String(); got != "*" {
		t.Errorf("String() = %v, want *", got)
	}
	if got := nr.getMap(); len(got) != 0 {
		t.Errorf("getMap() = %v, want empty map", got)
	}
}
