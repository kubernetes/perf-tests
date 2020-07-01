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
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"

	goerrors "github.com/go-errors/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
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

// LoadTestSuite creates test suite config from file specified by the given path.
func LoadTestSuite(path string) (api.TestSuite, error) {
	bin, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("test suite reading error: %v", err)
	}
	var testSuite api.TestSuite
	if err = decodeInto(bin, &testSuite); err != nil {
		return nil, err
	}
	if err = validateTestSuite(testSuite); err != nil {
		return nil, err
	}
	return testSuite, nil
}

func validateTestSuite(suite api.TestSuite) error {
	for _, scenario := range suite {
		// Scenario identifiers cannot contain underscores. This is because underscores
		// are used as separators in artifact filenames.
		if strings.Contains(scenario.Identifier, "_") {
			return fmt.Errorf("scenario identifiers cannot contain underscores: %q",
				scenario.Identifier)
		}
	}
	return nil
}

// LoadTestOverrides returns mapping from file specified by the given paths.
func LoadTestOverrides(paths []string) (map[string]interface{}, error) {
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

// GetMapping returns template variable mapping for the given ClusterLoaderConfig.
func GetMapping(clusterLoaderConfig *ClusterLoaderConfig) (map[string]interface{}, *errors.ErrorList) {
	mapping, err := LoadTestOverrides(clusterLoaderConfig.TestScenario.OverridePaths)
	if err != nil {
		return nil, errors.NewErrorList(fmt.Errorf("mapping creation error: %v", err))
	}
	mapping["Nodes"] = clusterLoaderConfig.ClusterConfig.Nodes
	envMapping, err := LoadCL2Envs()
	if err != nil {
		return nil, errors.NewErrorList(goerrors.Errorf("mapping creation error: %v", err))
	}
	err = MergeMappings(mapping, envMapping)
	if err != nil {
		return nil, errors.NewErrorList(fmt.Errorf("mapping creation error: %v", err))
	}
	return mapping, nil
}

// LoadCL2Envs returns mapping from the envs starting with CL2_ prefix.
func LoadCL2Envs() (map[string]interface{}, error) {
	mapping := make(map[string]interface{})
	for _, keyValue := range os.Environ() {
		if !strings.HasPrefix(keyValue, "CL2_") {
			continue
		}
		split := strings.Split(keyValue, "=")
		if len(split) != 2 {
			return nil, goerrors.Errorf("unparsable string in os.Eviron(): %v", keyValue)
		}
		key, value := split[0], split[1]
		mapping[key] = unpackStringValue(value)
	}
	return mapping, nil
}

func unpackStringValue(str string) interface{} {
	if v, err := strconv.ParseInt(str, 10, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(str, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(str); err == nil {
		return v
	}
	return str
}

// MergeMappings modifies map b to contain all new key=value pairs from b.
// It will return error in case of conflict, i.e. if exists key k for which a[k] != b[k]
func MergeMappings(a, b map[string]interface{}) error {
	for k, bv := range b {
		av, ok := a[k]
		if !ok {
			a[k] = bv
			continue
		}
		if !reflect.DeepEqual(av, bv) {
			return goerrors.Errorf("merge conflict for key '%v': old value=%v, new value=%v", k, av, bv)
		}
	}
	return nil
}
