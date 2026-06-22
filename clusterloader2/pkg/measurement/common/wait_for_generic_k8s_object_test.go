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

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
