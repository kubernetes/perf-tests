/*
Copyright 2021 The Kubernetes Authors.

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

package api

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

func isValid(errList field.ErrorList) bool {
	return len(errList) == 0
}

func TestVerifyQPSLoad(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    QPSLoad
		expected bool
	}{
		{
			name: "positive qps",
			input: QPSLoad{
				QPS: 10,
			},
			expected: true,
		},
		{
			name: "negative qps",
			input: QPSLoad{
				QPS: -1,
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateQPSLoad(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyRandomizedLoad(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    RandomizedLoad
		expected bool
	}{
		{
			name: "positive average qps",
			input: RandomizedLoad{
				AverageQPS: 10,
			},
			expected: true,
		},
		{
			name: "negative average qps",
			input: RandomizedLoad{
				AverageQPS: -1,
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateRandomizedLoad(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifySteppedLoad(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    SteppedLoad
		expected bool
	}{
		{
			name: "valid stepped load",
			input: SteppedLoad{
				BurstSize: 10,
				StepDelay: 1000,
			},
			expected: true,
		},
		{
			name: "negative burst size",
			input: SteppedLoad{
				BurstSize: -10,
				StepDelay: 1000,
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateSteppedLoad(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyTuningSet(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    TuningSet
		expected bool
	}{
		{
			name: "exactly one tuning set type",
			input: TuningSet{
				QPSLoad: &QPSLoad{
					QPS: 10,
				},
			},
			expected: true,
		},
		{
			name: "more than one tuning set type",
			input: TuningSet{
				QPSLoad: &QPSLoad{
					QPS: 10,
				},
				RandomizedLoad: &RandomizedLoad{
					AverageQPS: 10,
				},
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateTuningSet(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyNamespaceRange(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    NamespaceRange
		expected bool
	}{
		{
			name: "valid namespace range",
			input: NamespaceRange{
				Min: 0,
				Max: 10,
			},
			expected: true,
		},
		{
			name: "negative namespace range lower bound",
			input: NamespaceRange{
				Min: -1,
				Max: 10,
			},
			expected: false,
		},
		{
			name: "negative namespace range upper bound",
			input: NamespaceRange{
				Min: 10,
				Max: -1,
			},
			expected: false,
		},
		{
			name: "invalid namespace range",
			input: NamespaceRange{
				Min: 10,
				Max: 1,
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateNamespaceRange(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyPhase(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    Phase
		expected bool
	}{
		{
			name: "positive replicas",
			input: Phase{
				ReplicasPerNamespace: 10,
			},
			expected: true,
		},
		{
			name: "negative replicas",
			input: Phase{
				ReplicasPerNamespace: -10,
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validatePhase(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyMeasurement(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    Measurement
		expected bool
	}{
		{
			name: "Identifier specified only",
			input: Measurement{
				Identifier: "Measurement",
			},
			expected: true,
		},
		{
			name: "Instances specified only",
			input: Measurement{
				Instances: []*MeasurementInstanceConfig{
					{
						Identifier: "Measurement1",
					},
					{
						Identifier: "Measurement2",
					},
				},
			},
			expected: true,
		},
		{
			name: "Both identifier and instances specified",
			input: Measurement{
				Identifier: "Measurement",
				Instances: []*MeasurementInstanceConfig{
					{
						Identifier: "Measurement1",
					},
					{
						Identifier: "Measurement2",
					},
				},
			},
			expected: false,
		},
		{
			name:     "Identifier and instances empty",
			input:    Measurement{},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateMeasurement(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestVerifyStep(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    Step
		expected bool
	}{
		{
			name: "has phases and no measurements",
			input: Step{
				Phases: []*Phase{
					{
						ReplicasPerNamespace: 10,
					},
				},
			},
			expected: true,
		},
		{
			name: "has measurements and no phases",
			input: Step{
				Measurements: []*Measurement{
					{
						Method:     "test1",
						Identifier: "measurement1",
					},
				},
			},
			expected: true,
		},
		{
			name:     "no phases and no measurements",
			input:    Step{},
			expected: false,
		},
		{
			name: "has phases and measurements",
			input: Step{
				Phases: []*Phase{
					{
						ReplicasPerNamespace: 10,
					},
				},
				Measurements: []*Measurement{
					{
						Method:     "test1",
						Identifier: "measurement1",
					},
				},
			},
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator("", &Config{})
			got := isValid(v.validateStep(&test.input, field.NewPath("")))
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

// TODO(#1696): Remove deprecated automanagedNamespaces
func TestFileExists(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "file exists",
			input:    ".",
			expected: true,
		},
		{
			name:     "file does not exist",
			input:    "..",
			expected: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := NewConfigValidator(test.input, &Config{})
			got := v.fileExists("validation.go")
			if test.expected != got {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    Config
		expected *errors.ErrorList
	}{
		{
			name: "valid config",
			input: Config{
				AutomanagedNamespaces: 10,
				Namespace: NamespaceConfig{
					Number: 1,
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								ReplicasPerNamespace: 10,
							},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "negative automanaged namespaces",
			input: Config{
				AutomanagedNamespaces: -10,
				Namespace: NamespaceConfig{
					Number: 1,
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								ReplicasPerNamespace: 10,
							},
						},
					},
				},
			},
			expected: errors.NewErrorList(fmt.Errorf("automanagedNamespaces: Invalid value: -10: must be non-negative")),
		},
		{
			name: "non-positive number of namespaces",
			input: Config{
				AutomanagedNamespaces: 10,
				Namespace: NamespaceConfig{
					Number: 0,
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								ReplicasPerNamespace: 10,
							},
						},
					},
				},
			},
			expected: errors.NewErrorList(fmt.Errorf("namespace.number: Invalid value: 0: must be positive")),
		},
		{
			name: "zero number of steps",
			input: Config{
				AutomanagedNamespaces: 10,
				Namespace: NamespaceConfig{
					Number: 1,
				},
				Steps: []*Step{},
			},
			expected: errors.NewErrorList(fmt.Errorf("steps: Invalid value: 0: cannot be empty")),
		},
		{
			name: "non zero number of steps",
			input: Config{
				AutomanagedNamespaces: 10,
				Namespace: NamespaceConfig{
					Number: 1,
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								ReplicasPerNamespace: 10,
							},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "tuning set referenced in a phase has not been declared",
			input: Config{
				Namespace: NamespaceConfig{
					Number: 1,
				},
				TuningSets: []*TuningSet{
					{
						Name: "Sequence",
						QPSLoad: &QPSLoad{
							QPS: 10,
						},
					},
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								TuningSet:            "Sequence",
								ReplicasPerNamespace: 10,
							},
							{
								TuningSet: "Uniform5qps",
							},
						},
					},
				},
			},
			expected: errors.NewErrorList(fmt.Errorf("steps[0].phases[1].tuningSet: Invalid value: \"Uniform5qps\": tuning set referenced has not been declared")),
		},
		{
			name: "tuning set referenced in a phase has been declared",
			input: Config{
				Namespace: NamespaceConfig{
					Number: 1,
				},
				TuningSets: []*TuningSet{
					{
						Name: "Sequence",
						QPSLoad: &QPSLoad{
							QPS: 10,
						},
					},
				},
				Steps: []*Step{
					{
						Phases: []*Phase{
							{
								TuningSet:            "Sequence",
								ReplicasPerNamespace: 10,
							},
						},
					},
				},
			},
			expected: nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var failed bool
			v := NewConfigValidator("", &test.input)
			got := v.Validate()
			if test.expected == nil {
				if got != nil {
					failed = true
				}
			} else if test.expected.Error() != got.Error() {
				failed = true
			}

			if failed == true {
				t.Errorf("wanted: %v, got: %v", test.expected, got)
			}
		})
	}
}
