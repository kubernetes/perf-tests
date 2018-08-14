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
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TemplateProvider provides object templates. Templates in unstructured form
// are served by reading file from given path or by using cache if available.
type TemplateProvider struct {
	basepath string

	lock          sync.RWMutex
	templateCache map[string]*template.Template
}

// NewTemplateProvider creates new template provider.
func NewTemplateProvider(basepath string) *TemplateProvider {
	return &TemplateProvider{
		basepath:      basepath,
		templateCache: make(map[string]*template.Template),
	}
}

func (tp *TemplateProvider) getRawTemplate(path string) (*template.Template, error) {
	var err error
	tp.lock.RLock()
	raw, exists := tp.templateCache[path]
	tp.lock.RUnlock()
	if !exists {
		tp.lock.Lock()
		defer tp.lock.Unlock()
		// Recheck condition.
		raw, exists = tp.templateCache[path]
		if !exists {
			raw, err = template.ParseFiles(filepath.Join(tp.basepath, path))
			if err != nil {
				return nil, err
			}
			tp.templateCache[path] = raw
		}
	}
	return raw, nil
}

// TemplateToObject creates object from file specified by the given path
// or uses cached object if available. Template's placeholders are replaced based
// on provided mapping.
func (tp *TemplateProvider) TemplateToObject(path string, mapping map[string]interface{}) (*unstructured.Unstructured, error) {
	raw, err := tp.getRawTemplate(path)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	if err := raw.Execute(writer, mapping); err != nil {
		return nil, fmt.Errorf("replacing placeholders error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("flush error: %v", err)
	}
	return ConvertToObject(b.Bytes())
}
