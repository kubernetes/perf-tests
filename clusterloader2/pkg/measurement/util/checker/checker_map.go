/*
Copyright 2019 The Kubernetes Authors.

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

package checker

// Checker is a generic execution that can be stopped at any time.
type Checker interface {
	Stop()
}

// Map is a map of Checkers.
type Map map[string]Checker

// NewMap creates new checker map.
func NewMap() Map {
	return make(map[string]Checker)
}

// Dispose stops all checkers and cleans up the map.
func (cm Map) Dispose() {
	for _, c := range cm {
		c.Stop()
	}
	cm = make(map[string]Checker)
}

// Add adds checker to the checker map.
func (cm Map) Add(key string, c Checker) {
	if old, exists := cm[key]; exists {
		old.Stop()
	}
	cm[key] = c
}

// DeleteAndStop stops checker and deletes it if exists.
func (cm Map) DeleteAndStop(key string) bool {
	if old, exists := cm[key]; exists {
		old.Stop()
		delete(cm, key)
		return true
	}
	return false
}
