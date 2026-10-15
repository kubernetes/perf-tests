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

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourcesVersionsStateGetMissing(t *testing.T) {
	rs := newResourcesVersionsState()
	version, exists := rs.Get(ResourceTypeIdentifier{ObjectKind: "Pod"})
	assert.False(t, exists)
	assert.Equal(t, "0", version)
}

func TestResourcesVersionsStateSet(t *testing.T) {
	testCases := []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "single version", versions: []string{"42"}, want: "42"},
		{name: "increasing versions", versions: []string{"50", "100"}, want: "100"},
		{name: "older version is ignored", versions: []string{"100", "50"}, want: "100"},
		{name: "same version twice", versions: []string{"7", "7"}, want: "7"},
		{name: "max uint64", versions: []string{"18446744073709551615"}, want: "18446744073709551615"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rs := newResourcesVersionsState()
			id := ResourceTypeIdentifier{ObjectKind: "Pod"}
			for _, v := range tc.versions {
				require.NoError(t, rs.Set(id, v))
			}
			got, exists := rs.Get(id)
			assert.True(t, exists)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResourcesVersionsStateSetInvalid(t *testing.T) {
	invalid := []string{"", "abc", "-1", "1.5", "18446744073709551616"}
	for _, v := range invalid {
		t.Run(v, func(t *testing.T) {
			rs := newResourcesVersionsState()
			id := ResourceTypeIdentifier{ObjectKind: "Pod"}
			assert.Error(t, rs.Set(id, v))
			_, exists := rs.Get(id)
			assert.False(t, exists)
		})
	}
}

func TestResourcesVersionsStateInvalidKeepsPrevious(t *testing.T) {
	rs := newResourcesVersionsState()
	id := ResourceTypeIdentifier{ObjectKind: "Pod"}
	require.NoError(t, rs.Set(id, "10"))
	assert.Error(t, rs.Set(id, "abc"))
	got, exists := rs.Get(id)
	assert.True(t, exists)
	assert.Equal(t, "10", got)
}

func TestResourcesVersionsStateIdentifiersAreIndependent(t *testing.T) {
	rs := newResourcesVersionsState()
	pods := ResourceTypeIdentifier{ObjectKind: "Pod"}
	appsDeployments := ResourceTypeIdentifier{ObjectKind: "Deployment", APIGroup: "apps"}
	otherDeployments := ResourceTypeIdentifier{ObjectKind: "Deployment", APIGroup: "example.com"}

	require.NoError(t, rs.Set(pods, "10"))
	require.NoError(t, rs.Set(appsDeployments, "20"))

	got, exists := rs.Get(pods)
	assert.True(t, exists)
	assert.Equal(t, "10", got)

	got, exists = rs.Get(appsDeployments)
	assert.True(t, exists)
	assert.Equal(t, "20", got)

	_, exists = rs.Get(otherDeployments)
	assert.False(t, exists)
}
