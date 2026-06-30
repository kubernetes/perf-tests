/*
Copyright The Kubernetes Authors.

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

package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

func TestGetNamespaces_AllNamespaces(t *testing.T) {
	testCases := []struct {
		name             string
		namespacesPrefix string
		params           map[string]interface{}
		want             measurementutil.NamespacesRange
		wantErr          bool
	}{
		{
			name:             "allNamespaces true",
			namespacesPrefix: "default-prefix",
			params: map[string]interface{}{
				"namespaceRange": map[string]interface{}{
					"allNamespaces": true,
				},
			},
			want: measurementutil.NamespacesRange{
				AllNamespaces: true,
			},
		},
		{
			name:             "allNamespaces false with min and max",
			namespacesPrefix: "default-prefix",
			params: map[string]interface{}{
				"namespaceRange": map[string]interface{}{
					"allNamespaces": false,
					"min":           1,
					"max":           5,
					"prefix":        "custom-ns",
				},
			},
			want: measurementutil.NamespacesRange{
				AllNamespaces: false,
				Prefix:        "custom-ns",
				Min:           1,
				Max:           5,
			},
		},
		{
			name:             "allNamespaces omitted with min and max and default prefix",
			namespacesPrefix: "default-prefix",
			params: map[string]interface{}{
				"namespaceRange": map[string]interface{}{
					"min": 2,
					"max": 4,
				},
			},
			want: measurementutil.NamespacesRange{
				AllNamespaces: false,
				Prefix:        "default-prefix",
				Min:           2,
				Max:           4,
			},
		},
		{
			name:             "no namespaceRange provided (cluster-scoped)",
			namespacesPrefix: "default-prefix",
			params:           map[string]interface{}{},
			want: measurementutil.NamespacesRange{
				Prefix: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getNamespaces(tc.namespacesPrefix, tc.params)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestGetLabelSelector(t *testing.T) {
	testCases := []struct {
		name    string
		params  map[string]interface{}
		want    string
		wantErr bool
	}{
		{
			name:   "labelSelector omitted",
			params: map[string]interface{}{},
			want:   labels.Everything().String(),
		},
		{
			name: "labelSelector provided",
			params: map[string]interface{}{
				"labelSelector": "app=worker,tier in (backend,cache)",
			},
			want: "app=worker,tier in (backend,cache)",
		},
		{
			name: "invalid labelSelector",
			params: map[string]interface{}{
				"labelSelector": "app in (",
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getLabelSelector(tc.params)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got.String())
			}
		})
	}
}

func TestWaitForGenericK8sObjectsMeasurement_Execute_OmittedConditions(t *testing.T) {
	fakeClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "", Version: "v1", Resource: "pods"}: "PodList",
	})

	pod1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "pod-1",
				"namespace": "test-ns-0",
			},
		},
	}
	pod2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "pod-2",
				"namespace": "test-ns-0",
			},
		},
	}

	ctx := context.Background()
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	_, err := fakeClient.Resource(gvr).Namespace("test-ns-0").Create(ctx, pod1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.Resource(gvr).Namespace("test-ns-0").Create(ctx, pod2, metav1.CreateOptions{})
	assert.NoError(t, err)

	multiDynamic := framework.NewMultiDynamicClientFromClients(fakeClient)
	clusterFramework := framework.NewFrameworkFromClients(nil, multiDynamic)

	m := createWaitForGenericK8sObjectsMeasurement()
	config := &measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"objectGroup":           "",
			"objectVersion":         "v1",
			"objectResource":        "pods",
			"minDesiredObjectCount": 2,
			"refreshInterval":       "100ms",
			"timeout":               "1s",
			"namespaceRange": map[string]interface{}{
				"prefix": "test-ns",
				"min":    0,
				"max":    0,
			},
		},
	}

	summaries, err := m.Execute(config)
	assert.NoError(t, err)
	assert.Nil(t, summaries)
}
