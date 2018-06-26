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

package validation

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/perf-tests/clusterloader2/api"
)

func isValid(errList field.ErrorList) bool {
	return len(errList) == 0
}

func TestVerifyQpsLoad(t *testing.T) {
	for i, test := range []struct {
		input    api.QpsLoad
		expected bool
	}{
		{
			input: api.QpsLoad{
				Qps: 10,
			},
			expected: true,
		},
		{
			input: api.QpsLoad{
				Qps: -1,
			},
			expected: false,
		},
	} {
		got := isValid(verifyQpsLoad(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyRandomizedLoad(t *testing.T) {
	for i, test := range []struct {
		input    api.RandomizedLoad
		expected bool
	}{
		{
			input: api.RandomizedLoad{
				AverageQps: 10,
			},
			expected: true,
		},
		{
			input: api.RandomizedLoad{
				AverageQps: -1,
			},
			expected: false,
		},
	} {
		got := isValid(verifyRandomizedLoad(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifySteppedLoad(t *testing.T) {
	for i, test := range []struct {
		input    api.SteppedLoad
		expected bool
	}{
		{
			input: api.SteppedLoad{
				BurstSize: 10,
				StepDelay: time.Duration(10),
			},
			expected: true,
		},
		{
			input: api.SteppedLoad{
				BurstSize: -10,
				StepDelay: time.Duration(10),
			},
			expected: false,
		},
	} {
		got := isValid(verifySteppedLoad(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyTuningSet(t *testing.T) {
	for i, test := range []struct {
		input    api.TuningSet
		expected bool
	}{
		{
			input: api.TuningSet{
				QpsLoad: &api.QpsLoad{
					Qps: 10,
				},
			},
			expected: true,
		},
		{
			input: api.TuningSet{
				QpsLoad: &api.QpsLoad{
					Qps: 10,
				},
				RandomizedLoad: &api.RandomizedLoad{
					AverageQps: 10,
				},
			},
			expected: false,
		},
	} {
		got := isValid(verifyTuningSet(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyNamespaceRange(t *testing.T) {
	for i, test := range []struct {
		input    api.NamespaceRange
		expected bool
	}{
		{
			input: api.NamespaceRange{
				Min: 0,
				Max: 10,
			},
			expected: true,
		},
		{
			input: api.NamespaceRange{
				Min: -1,
				Max: 10,
			},
			expected: false,
		},
		{
			input: api.NamespaceRange{
				Min: 10,
				Max: -1,
			},
			expected: false,
		},
		{
			input: api.NamespaceRange{
				Min: 10,
				Max: 1,
			},
			expected: false,
		},
	} {
		got := isValid(verifyNamespaceRange(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyPhase(t *testing.T) {
	for i, test := range []struct {
		input    api.Phase
		expected bool
	}{
		{
			input: api.Phase{
				ReplicasPerNamespace: 10,
			},
			expected: true,
		},
		{
			input: api.Phase{
				ReplicasPerNamespace: -10,
			},
			expected: false,
		},
	} {
		got := isValid(verifyPhase(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyStep(t *testing.T) {
	for i, test := range []struct {
		input    api.Step
		expected bool
	}{
		{
			input: api.Step{
				Phases: []api.Phase{
					{
						ReplicasPerNamespace: 10,
					},
				},
			},
			expected: true,
		},
		{
			input: api.Step{
				Measurements: []api.Measurement{
					{
						Method: "test1",
					},
				},
			},
			expected: true,
		},
		{
			input:    api.Step{},
			expected: false,
		},
		{
			input: api.Step{
				Phases: []api.Phase{
					{
						ReplicasPerNamespace: 10,
					},
				},
				Measurements: []api.Measurement{
					{
						Method: "test1",
					},
				},
			},
			expected: false,
		},
	} {
		got := isValid(verifyStep(&test.input, field.NewPath("")))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}

func TestVerifyConfig(t *testing.T) {
	for i, test := range []struct {
		input    api.Config
		expected bool
	}{
		{
			input: api.Config{
				AutomanagedNamespaces: 10,
			},
			expected: true,
		},
		{
			input: api.Config{
				AutomanagedNamespaces: -10,
			},
			expected: false,
		},
	} {
		got := isValid(VerifyConfig(&test.input))
		if test.expected != got {
			t.Errorf("Verify nr %d = %v, want %v", i, got, test.expected)
		}
	}
}
