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
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
)

// Modifier mutates provided test
type Modifier interface {
	ChangeTest(*api.Config)
}

// InitFlags allows setting configuration with flags
func InitFlags(m *config.ModifierConfig) {
	flags.StringArrayVar(&m.SkipSteps, "skip-steps", []string{}, "Name of steps to skip in test")
}

// NewModifier creates new Modifier according to provided configuration
func NewModifier(m *config.ModifierConfig) Modifier {
	return &simpleModifier{skipSteps: m.SkipSteps}
}

type simpleModifier struct {
	skipSteps []string
}

// Ensuring that simpleModifier implements Modifier interface
var _ Modifier = &simpleModifier{}

func (m *simpleModifier) ChangeTest(c *api.Config) {
	steps := c.Steps
	c.Steps = []api.Step{}
	for _, s := range steps {
		ignored := false
		for _, i := range m.skipSteps {
			if i == s.Name {
				ignored = true
				break
			}
		}
		if !ignored {
			c.Steps = append(c.Steps, s)
		}
	}
}
