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

var testIdentifier = InstancesIdentifier{
	Basename:   "test-deployment",
	ObjectKind: "Deployment",
	APIGroup:   "apps",
}

func TestNamespacesStateGetMissingNamespace(t *testing.T) {
	ns := newNamespacesState()
	instances, exists := ns.Get("default", testIdentifier)
	assert.False(t, exists)
	assert.Nil(t, instances)
}

func TestNamespacesStateSetAndGet(t *testing.T) {
	ns := newNamespacesState()
	want := &InstancesState{DesiredReplicaCount: 3, CurrentReplicaCount: 1}
	ns.Set("default", testIdentifier, want)

	got, exists := ns.Get("default", testIdentifier)
	assert.True(t, exists)
	assert.Same(t, want, got)
}

func TestNamespacesStateGetMissingIdentifier(t *testing.T) {
	ns := newNamespacesState()
	ns.Set("default", testIdentifier, &InstancesState{DesiredReplicaCount: 1})

	other := InstancesIdentifier{Basename: "other", ObjectKind: "Deployment", APIGroup: "apps"}
	instances, exists := ns.Get("default", other)
	assert.False(t, exists)
	assert.Nil(t, instances)
}

func TestNamespacesStateSetOverwrites(t *testing.T) {
	ns := newNamespacesState()
	first := &InstancesState{DesiredReplicaCount: 1}
	second := &InstancesState{DesiredReplicaCount: 5}
	ns.Set("default", testIdentifier, first)
	ns.Set("default", testIdentifier, second)

	got, exists := ns.Get("default", testIdentifier)
	assert.True(t, exists)
	assert.Same(t, second, got)
}

func TestNamespacesStateNamespacesAreIndependent(t *testing.T) {
	ns := newNamespacesState()
	inA := &InstancesState{DesiredReplicaCount: 1}
	inB := &InstancesState{DesiredReplicaCount: 2}
	ns.Set("namespace-a", testIdentifier, inA)
	ns.Set("namespace-b", testIdentifier, inB)

	require.NoError(t, ns.Delete("namespace-a", testIdentifier))

	_, exists := ns.Get("namespace-a", testIdentifier)
	assert.False(t, exists)

	got, exists := ns.Get("namespace-b", testIdentifier)
	assert.True(t, exists)
	assert.Same(t, inB, got)
}

func TestNamespacesStateDelete(t *testing.T) {
	ns := newNamespacesState()
	ns.Set("default", testIdentifier, &InstancesState{DesiredReplicaCount: 1})

	require.NoError(t, ns.Delete("default", testIdentifier))

	_, exists := ns.Get("default", testIdentifier)
	assert.False(t, exists)
}

func TestNamespacesStateDeleteMissingNamespace(t *testing.T) {
	ns := newNamespacesState()
	assert.Error(t, ns.Delete("default", testIdentifier))
}

func TestNamespacesStateDeleteMissingIdentifier(t *testing.T) {
	ns := newNamespacesState()
	ns.Set("default", testIdentifier, &InstancesState{DesiredReplicaCount: 1})

	other := InstancesIdentifier{Basename: "other", ObjectKind: "Deployment", APIGroup: "apps"}
	assert.Error(t, ns.Delete("default", other))

	_, exists := ns.Get("default", testIdentifier)
	assert.True(t, exists)
}
