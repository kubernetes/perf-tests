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

package util

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestDynamicObjectStore_AllNamespaces(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Group:    "test.group",
		Version:  "v1",
		Resource: "items",
	}

	obj1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "item-1",
				"namespace": "ns-1",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{},
			},
		},
	}
	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "item-2",
				"namespace": "ns-2",
			},
			"status": map[string]interface{}{
				"conditions": []interface{}{},
			},
		},
	}

	tests := []struct {
		name          string
		namespaces    map[string]bool
		wantNames     []string
		wantAllNsFlag bool
	}{
		{
			name:          "allNamespaces enabled when namespaces map is empty",
			namespaces:    map[string]bool{},
			wantNames:     []string{"ns-1/item-1", "ns-2/item-2"},
			wantAllNsFlag: true,
		},
		{
			name:          "allNamespaces disabled when specific namespaces are provided",
			namespaces:    map[string]bool{"ns-1": true},
			wantNames:     []string{"ns-1/item-1"},
			wantAllNsFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				gvr: "ItemsList",
			})

			_, err := dynamicClient.Resource(gvr).Namespace("ns-1").Create(ctx, obj1, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = dynamicClient.Resource(gvr).Namespace("ns-2").Create(ctx, obj2, metav1.CreateOptions{})
			require.NoError(t, err)

			store, err := NewDynamicObjectStore(ctx, dynamicClient, gvr, tt.namespaces)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllNsFlag, store.allNamespaces)

			results, err := store.ListObjectSimplifications()
			require.NoError(t, err)

			gotNames := make([]string, 0, len(results))
			for _, r := range results {
				gotNames = append(gotNames, r.String())
			}
			assert.ElementsMatch(t, tt.wantNames, gotNames)
		})
	}
}
