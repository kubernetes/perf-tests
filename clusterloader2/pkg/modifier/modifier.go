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

package modifier

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
)

// Modifier mutates provided test
type Modifier interface {
	ChangeTest(*api.Config) error
}

// InitFlags allows setting configuration with flags
func InitFlags(m *config.ModifierConfig) {
	flags.StringArrayVar(&m.OverwriteTestConfig, "overwrite-test-config", []string{}, "Overwrite test config with specific value, used as parameter1.parameter2=value, for example 'Namespace.Prefix=custom-prefix'")
	flags.StringArrayVar(&m.SkipSteps, "skip-steps", []string{}, "Name of steps to skip in test")
}

// NewModifier creates new Modifier according to provided configuration
func NewModifier(m *config.ModifierConfig) Modifier {
	return &simpleModifier{overwriteTestConfig: m.OverwriteTestConfig, skipSteps: m.SkipSteps}
}

type simpleModifier struct {
	overwriteTestConfig []string
	skipSteps           []string
}

// Ensuring that simpleModifier implements Modifier interface
var _ Modifier = &simpleModifier{}

func (m *simpleModifier) ChangeTest(c *api.Config) error {
	m.modifySkipSteps(c)
	return m.modifyOverwrite(c)
}

func (m *simpleModifier) modifySkipSteps(c *api.Config) {
	steps := c.Steps
	c.Steps = []*api.Step{}
	for _, s := range steps {
		ignored := false
		for _, i := range m.skipSteps {
			if i == s.Name {
				ignored = true
				klog.V(3).Infof("Ignoring step %s", s.Name)
				break
			}
		}
		if !ignored {
			c.Steps = append(c.Steps, s)
		}
	}
}

func (m *simpleModifier) modifyOverwrite(c *api.Config) error {
	for _, o := range m.overwriteTestConfig {
		kv := strings.Split(o, "=")
		if len(kv) != 2 {
			return fmt.Errorf("not a key=value pair: '%s'", o)
		}
		k, v := kv[0], kv[1]

		parameterPath := strings.Split(k, ".")
		curValue := reflect.ValueOf(c).Elem()
		for _, p := range parameterPath {
			curValue = curValue.FieldByName(p)
			if !curValue.IsValid() {
				return fmt.Errorf("cannot overwrite config for key '%s'. Path does not exist", k)
			}
			// We want to dereference pointers if any happen along the way
			if curValue.Kind() == reflect.Ptr {
				// If path came across ptr to nil, we need to create zero value before dereferencing
				if curValue.IsNil() {
					expectedType := curValue.Type().Elem()
					pointerToZeroValue := reflect.New(expectedType)
					curValue.Set(pointerToZeroValue)
				}
				curValue = curValue.Elem()
			}
		}
		err := m.overwriteValue(curValue, v, k)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *simpleModifier) overwriteValue(node reflect.Value, v, k string) error {
	switch node.Kind() {
	case reflect.Bool:
		boolV, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("test config overwrite error: Cannot parse '%s' for key '%s' to bool: %v", v, k, err)
		}
		klog.V(2).Infof("Setting bool value '%t' for key '%s'", boolV, k)
		node.SetBool(boolV)
	case reflect.String:
		klog.V(2).Infof("Setting string value '%s' for key '%s'", v, k)
		node.SetString(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intV, err := strconv.ParseInt(v, 10, node.Type().Bits())
		if err != nil {
			return fmt.Errorf("test config overwrite error: Cannot parse '%s' for key '%s' to int: %v", v, k, err)
		}
		klog.V(2).Infof("Setting int value '%d' for key '%s'", intV, k)
		node.SetInt(intV)
	default:
		return fmt.Errorf("unsupported kind: %v for key %s", node.Kind(), k)
	}
	return nil
}
