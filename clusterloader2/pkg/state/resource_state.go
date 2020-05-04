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
	"strconv"
	"sync"
)

// ResourceTypeIdentifier is a unique identifier for a resource type.
type ResourceTypeIdentifier struct {
	ObjectKind string
	APIGroup   string
}

// ResourcesVersionsState represents most recent resources versions for a given object types.
// These versions are available only for resource types of objects that were create/updated
// by the clusterloader.
type ResourcesVersionsState struct {
	lock              sync.RWMutex
	resourcesVersions map[ResourceTypeIdentifier]uint64
}

// newResourcesVersionState creates new resources versions state.
func newResourcesVersionsState() *ResourcesVersionsState {
	return &ResourcesVersionsState{
		resourcesVersions: make(map[ResourceTypeIdentifier]uint64),
	}
}

// Get returns state of current resource version.
func (rs *ResourcesVersionsState) Get(identifier ResourceTypeIdentifier) (string, bool) {
	rs.lock.RLock()
	defer rs.lock.RUnlock()
	version, exists := rs.resourcesVersions[identifier]
	if !exists {
		return "0", false
	}
	return strconv.FormatUint(version, 10), true
}

// Set stores information about current resource version.
func (rs *ResourcesVersionsState) Set(identifier ResourceTypeIdentifier, resourceVersion string) error {
	rs.lock.Lock()
	defer rs.lock.Unlock()
	version, err := strconv.ParseUint(resourceVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("incorrect version format")
	}
	if current, exists := rs.resourcesVersions[identifier]; exists && current > version {
		version = current
	}
	rs.resourcesVersions[identifier] = version
	return nil
}
