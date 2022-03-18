/*
Copyright 2020 The Kubernetes Authors.

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
	"sync"
)

// podMetadata contains metadata about a single pod
type podMetadata struct {
	stateless bool
}

// PodsMetadata store metadata about pods.
type PodsMetadata struct {
	name     string
	lock     sync.Mutex
	metadata map[string]*podMetadata
}

// NewPodsMetadata created new PodsMetadata instance.
func NewPodsMetadata(name string) *PodsMetadata {
	return &PodsMetadata{
		name:     name,
		metadata: make(map[string]*podMetadata),
	}
}

// SetStateless marks a given pod as stateless.
func (o *PodsMetadata) SetStateless(key string, stateless bool) {
	o.lock.Lock()
	defer o.lock.Unlock()
	if meta, ok := o.metadata[key]; ok {
		meta.stateless = stateless
		return
	}
	o.metadata[key] = &podMetadata{
		stateless: stateless,
	}
}

// FilterStateless returns true iff a pod associated with a given key is
// marked as stateless.
func (o *PodsMetadata) FilterStateless(key string) bool {
	o.lock.Lock()
	defer o.lock.Unlock()
	meta, ok := o.metadata[key]
	return ok && meta.stateless
}

// FilterStateful returns true iff a pod associated with a given key is
// not marked as stateless.
func (o *PodsMetadata) FilterStateful(key string) bool {
	return !o.FilterStateless(key)
}
