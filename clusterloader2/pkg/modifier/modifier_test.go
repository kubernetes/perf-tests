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
	"k8s.io/perf-tests/clusterloader2/api"
	"testing"
)

func defaultConfig() *api.Config {
	return &api.Config{
		Name: "test-modifier",
		Steps: []api.Step{{
			Name: "eternal",
		}},
	}
}

func TestModifier(t *testing.T) {
	m := &simpleModifier{skipSteps: []string{"skip-me", "skip me"}}
	testCase := [][]string{{}, {"skip-me"}, {"skip-me", "skip me"}}
	for _, d := range testCase {
		c := defaultConfig()
		for _, s := range d {
			c.Steps = append(c.Steps, api.Step{Name: s})
		}
		m.ChangeTest(c)
		if len(c.Steps) != 1 {
			t.Errorf("For test case %v: Changed test config in unexpected way, expected to have 1 step, but was %d steps", d, len(c.Steps))
		}
		if c.Steps[0].Name != "eternal" {
			t.Errorf("For test case %v: Changed test in unexpected way, expected to have 1 step with name 'eternal', but was %s", d, c.Steps[0].Name)
		}
	}

}
