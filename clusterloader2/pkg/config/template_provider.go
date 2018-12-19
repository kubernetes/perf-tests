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
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sync"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/perf-tests/clusterloader2/api"
)

// TemplateProvider provides object templates. Templates in unstructured form
// are served by reading file from given path or by using cache if available.
type TemplateProvider struct {
	basepath string

	binLock  sync.RWMutex
	binCache map[string][]byte

	templateLock  sync.RWMutex
	templateCache map[string]*template.Template
}

// NewTemplateProvider creates new template provider.
func NewTemplateProvider(basepath string) *TemplateProvider {
	return &TemplateProvider{
		basepath:      basepath,
		binCache:      make(map[string][]byte),
		templateCache: make(map[string]*template.Template),
	}
}

func (tp *TemplateProvider) getRaw(path string) ([]byte, error) {
	tp.binLock.RLock()
	bin, exists := tp.binCache[path]
	tp.binLock.RUnlock()
	if !exists {
		tp.binLock.Lock()
		defer tp.binLock.Unlock()
		// Recheck condition.
		bin, exists = tp.binCache[path]
		if !exists {
			var err error
			bin, err = ioutil.ReadFile(filepath.Join(tp.basepath, path))
			if err != nil {
				return []byte{}, fmt.Errorf("reading error: %v", err)
			}
			tp.binCache[path] = bin
		}
	}
	return bin, nil
}

// RawToObject creates object from file specified by the given path
// or uses cached object if available.
func (tp *TemplateProvider) RawToObject(path string) (*unstructured.Unstructured, error) {
	bin, err := tp.getRaw(path)
	if err != nil {
		return nil, err
	}
	// Removing all placeholder from template.
	// This needs to be done due to placeholders not being valid yaml.
	r, err := regexp.Compile("\\{\\{.*\\}\\}")
	if err != nil {
		return nil, fmt.Errorf("regexp creation error: %v", err)
	}
	bin = r.ReplaceAll(bin, []byte{})
	return convertToObject(bin)
}

func (tp *TemplateProvider) getRawTemplate(path string) (*template.Template, error) {
	tp.templateLock.RLock()
	raw, exists := tp.templateCache[path]
	tp.templateLock.RUnlock()
	if !exists {
		tp.templateLock.Lock()
		defer tp.templateLock.Unlock()
		// Recheck condition.
		raw, exists = tp.templateCache[path]
		if !exists {
			bin, err := tp.getRaw(path)
			if err != nil {
				return nil, err
			}
			raw = template.New("").Funcs(GetFuncs())
			raw, err = raw.Parse(string(bin))
			if err != nil {
				return nil, fmt.Errorf("parsing error: %v", err)
			}
			tp.templateCache[path] = raw
		}
	}
	return raw, nil
}

func (tp *TemplateProvider) getMappedTemplate(path string, mapping map[string]interface{}) ([]byte, error) {
	raw, err := tp.getRawTemplate(path)
	if err != nil {
		return []byte{}, err
	}
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	if err := raw.Execute(writer, mapping); err != nil {
		return []byte{}, fmt.Errorf("replacing placeholders error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		return []byte{}, fmt.Errorf("flush error: %v", err)
	}
	return b.Bytes(), nil
}

// TemplateToObject creates object from file specified by the given path
// or uses cached object if available. Template's placeholders are replaced based
// on provided mapping.
func (tp *TemplateProvider) TemplateToObject(path string, mapping map[string]interface{}) (*unstructured.Unstructured, error) {
	b, err := tp.getMappedTemplate(path, mapping)
	if err != nil {
		return nil, err
	}
	return convertToObject(b)
}

// TemplateToConfig creates test config from file specified by the given path.
// Template's placeholders are replaced based on provided mapping.
func (tp *TemplateProvider) TemplateToConfig(path string, mapping map[string]interface{}) (*api.Config, error) {
	b, err := tp.getMappedTemplate(path, mapping)
	if err != nil {
		return nil, err
	}
	return convertToConfig(b)
}

// TemplateInto decodes template specified by the given path into given structure.
func (tp *TemplateProvider) TemplateInto(path string, mapping map[string]interface{}, obj interface{}) error {
	b, err := tp.getMappedTemplate(path, mapping)
	if err != nil {
		return err
	}
	return decodeInto(b, obj)
}

// GetOverridesMapping returns mapping from file specified by the given paths.
func GetOverridesMapping(paths []string) (map[string]interface{}, error) {
	mapping := make(map[string]interface{})
	for _, path := range paths {
		bin, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("test overrides reading error: %v", err)
		}
		tmpMapping := make(map[string]interface{})
		if err = decodeInto(bin, &tmpMapping); err != nil {
			return nil, fmt.Errorf("test overrides unmarshalling error: %v", err)
		}
		// Merge tmpMapping into mapping.
		for k, v := range tmpMapping {
			mapping[k] = v
		}
	}
	return mapping, nil
}
