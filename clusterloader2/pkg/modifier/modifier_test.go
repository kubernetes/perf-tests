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
	"reflect"
	"testing"

	"k8s.io/perf-tests/clusterloader2/api"
)

func defaultConfig() *api.Config {
	return &api.Config{
		Name: "test-modifier",
		Steps: []*api.Step{{
			Name: "eternal",
		}},
	}
}

func TestModifySkipSteps(t *testing.T) {
	m := &simpleModifier{skipSteps: []string{"skip-me", "skip me"}}
	testCase := [][]string{{}, {"skip-me"}, {"skip-me", "skip me"}}
	for _, d := range testCase {
		c := defaultConfig()
		for _, s := range d {
			c.Steps = append(c.Steps, &api.Step{Name: s})
		}
		m.modifySkipSteps(c)
		if len(c.Steps) != 1 {
			t.Errorf("For test case %v: Changed test config in unexpected way, expected to have 1 step, but was %d steps", d, len(c.Steps))
		}
		if c.Steps[0].Name != "eternal" {
			t.Errorf("For test case %v: Changed test in unexpected way, expected to have 1 step with name 'eternal', but was %s", d, c.Steps[0].Name)
		}
	}
}

func TestModifyOverwrite(t *testing.T) {
	testCase := []struct {
		overwrite []string
		expected  *api.Config
		err       string
	}{
		{overwrite: []string{}, expected: defaultConfig()},
		{
			overwrite: []string{"Namespace.Prefix=overwritten-prefix"},
			expected: &api.Config{
				Name: "test-modifier",
				Steps: []*api.Step{{
					Name: "eternal",
				}},
				Namespace: api.NamespaceConfig{
					Prefix: "overwritten-prefix",
				}},
		},
		{
			overwrite: []string{"Namespace.EnableExistingNamespaces=true"},
			expected: &api.Config{
				Name: "test-modifier",
				Steps: []*api.Step{{
					Name: "eternal",
				}},
				Namespace: api.NamespaceConfig{
					// Golang does not allow to take a pointer to '&true'
					EnableExistingNamespaces: &[]bool{true}[0],
				}},
		},
		{
			overwrite: []string{"Namespace.Number=42"},
			expected: &api.Config{
				Name: "test-modifier",
				Steps: []*api.Step{{
					Name: "eternal",
				}},
				Namespace: api.NamespaceConfig{
					Number: 42,
				}},
		},
		{
			overwrite: []string{"Namespace.Number=214748364800"},
			err:       "test config overwrite error: Cannot parse '214748364800' for key 'Namespace.Number' to int: strconv.ParseInt: parsing \"214748364800\": value out of range",
		},
		{
			overwrite: []string{"NotExistingParameter=123"},
			err:       "cannot overwrite config for key 'NotExistingParameter'. Path does not exist",
		},
		{
			overwrite: []string{"NotAPair"},
			err:       "not a key=value pair: 'NotAPair'",
		},
	}
	for _, d := range testCase {
		m := &simpleModifier{overwriteTestConfig: d.overwrite}
		c := defaultConfig()
		err := m.modifyOverwrite(c)
		if d.expected != nil {
			if err != nil {
				t.Errorf("For test case %v: Expected succes, however error %v happened", d.overwrite, err)
			}
			if !reflect.DeepEqual(c, d.expected) {
				t.Errorf("For test case %v: Changed test in unexpected way, was '%v' but expected '%v'", d.overwrite, c, d.expected)
			}
		} else {
			if err == nil {
				t.Errorf("For test case %v: Expected error, however it did not happen", d.overwrite)
			}
			if err.Error() != d.err {
				t.Errorf("For test case %v: Returned unexpected error '%s', expected error '%s'", d.overwrite, err, d.err)
			}
		}
	}
}
