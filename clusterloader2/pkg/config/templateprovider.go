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

package config

import (
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TemplateProvider provides object templates. Templates in unstructured form
// are served by reading file from given path or by using cache if available.
type TemplateProvider struct {
	lock          sync.RWMutex
	templateCache map[string]*unstructured.Unstructured
}

// NewTemplateProvider creates new template provider.
func NewTemplateProvider() *TemplateProvider {
	return &TemplateProvider{
		templateCache: make(map[string]*unstructured.Unstructured),
	}
}

// GetTemplate creates object template from file specified by the given path
// or uses cached object if available.
func (ts *TemplateProvider) GetTemplate(path string) (*unstructured.Unstructured, error) {
	var err error
	ts.lock.RLock()
	obj, exists := ts.templateCache[path]
	ts.lock.RUnlock()
	if !exists {
		ts.lock.Lock()
		defer ts.lock.Unlock()
		// Recheck condition.
		obj, exists = ts.templateCache[path]
		if !exists {
			obj, err = ReadTemplate(path)
			if err != nil {
				return nil, err
			}
			ts.templateCache[path] = obj
		}
	}
	return obj.DeepCopy(), nil
}
