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

package state

import (
	"fmt"
	"sync"

	"k8s.io/perf-tests/clusterloader2/api"
)

// InstancesState represents state of object replicas.
type InstancesState struct {
	DesiredReplicaCount int32
	CurrentReplicaCount int32
	Object              *api.Object
}

// InstancesIdentifier is a unique identifier for object replicas group
type InstancesIdentifier struct {
	Basename   string
	ObjectKind string
	APIGroup   string
}

// namespaceState represents state of a single namespace.
type namespaceState map[InstancesIdentifier]*InstancesState

// NamespacesState represents state of all used namespaces.
type NamespacesState struct {
	lock            sync.RWMutex
	namespaceStates map[string]namespaceState
}

// newNamespacesState creates new namespaces state.
func newNamespacesState() *NamespacesState {
	return &NamespacesState{
		namespaceStates: make(map[string]namespaceState),
	}
}

// Get returns state of object instances -
// number of existing replicas and its configuration.
func (ns *NamespacesState) Get(namespace string, identifier InstancesIdentifier) (*InstancesState, bool) {
	ns.lock.RLock()
	defer ns.lock.RUnlock()
	namespaceState, exists := ns.namespaceStates[namespace]
	if !exists {
		return nil, false
	}
	instances, exists := namespaceState[identifier]
	return instances, exists
}

// Set stores information about object instances state
// to test state.
func (ns *NamespacesState) Set(namespace string, identifier InstancesIdentifier, instances *InstancesState) {
	ns.lock.Lock()
	defer ns.lock.Unlock()
	_, exists := ns.namespaceStates[namespace]
	if !exists {
		ns.namespaceStates[namespace] = make(namespaceState)
	}
	ns.namespaceStates[namespace][identifier] = instances
}

// Delete removes information about given instances.
// It there is no information for given object it is assumed that
// there are no object replicas.
func (ns *NamespacesState) Delete(namespace string, identifier InstancesIdentifier) error {
	ns.lock.Lock()
	defer ns.lock.Unlock()
	namespaceState, exists := ns.namespaceStates[namespace]
	if !exists {
		return fmt.Errorf("namespace %v not found", namespace)
	}
	_, exists = namespaceState[identifier]
	if !exists {
		return fmt.Errorf("no instances of %+v found in %v namespace", identifier, namespace)
	}
	delete(namespaceState, identifier)
	return nil
}
