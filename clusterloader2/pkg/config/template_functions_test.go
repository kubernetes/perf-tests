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

package config

import (
	"bytes"
	"testing"
	"text/template"
)

func TestChance(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if chance(0) {
			t.Errorf("Expected change(0) to always equal 'false', equaled 'true' at iteration %d", i)
		}
	}
	for i := 0; i < 1000; i++ {
		if !chance(1) {
			t.Errorf("Expected change(1) to always equal 'true', equaled 'false' at iteration %d", i)
		}
	}
	type testCase struct {
		template string
		expected string
	}
	testCases := []testCase{
		{
			template: "{{if Chance  1}}FooBar{{end}}",
			expected: "FooBar",
		},
		{
			template: "{{if not (Chance 0)}}FooBar{{end}}",
			expected: "FooBar",
		},
		{
			template: "{{if Chance 1}}Foo{{else}}Bar{{end}}",
			expected: "Foo",
		},
		{
			template: "{{if Chance 0}}Foo{{else}}Bar{{end}}",
			expected: "Bar",
		},
	}
	for _, tc := range testCases {
		tpl, err := template.New("").Funcs(GetFuncs()).Parse(tc.template)
		if err != nil {
			t.Errorf("Error parsing template %s, errror: %v", tc.template, err)
		}
		var executed bytes.Buffer
		if err := tpl.Execute(&executed, nil); err != nil {
			t.Errorf("Error executing template %s, error: %v", tc.template, err)
		}
		if executed.String() != tc.expected {
			t.Errorf("Error parsing template %s, expected %s but was %s", tc.template, tc.expected, executed.String())
		}
	}
}
