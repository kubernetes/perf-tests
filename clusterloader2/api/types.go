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

package api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// TestSuite defines list of test scenarios to be run.
type TestSuite []TestScenario

// TestScenario defines customized test to be run.
type TestScenario struct {
	// Identifier is a unique test scenario name across test suite.
	Identifier string `json:"identifier"`
	// ConfigPath defines path to the file containing a single Config definition.
	ConfigPath string `json:"configPath"`
	// OverridePaths defines what override files should be applied
	// to the config specified by the ConfigPath. This supersedes the global
	// config provided by ClusterLoaderConfig.
	OverridePaths []string `json:"overridePaths"`
}

// Config is a structure that represents configuration
// for a single test scenario.
type Config struct {
	// Name of the test case.
	Name string `json:"name"`
	// TODO(#1696): Clean up after removing automanagedNamespaces
	AutomanagedNamespaces int32 `json:"automanagedNamespaces,omitempty"`
	// Namespace is a structure for namespace configuration.
	Namespace NamespaceConfig `json:"namespace"`
	// Steps is a sequence of test steps executed in serial.
	Steps []*Step `json:"steps"`
	// TuningSets is a collection of tuning sets that can be used by steps.
	TuningSets []*TuningSet `json:"tuningSets"`
	// ChaosMonkey is a config for simulated component failures.
	ChaosMonkey ChaosMonkeyConfig `json:"chaosMonkey"`
}

// Step represents a unit of work in ClusterLoader2. It can be either:
// - a collection of measurements,
// - a collection of phases,
// - a module (sequence of steps).
// Exactly one field (Phases or Measurements or Module) should be non-empty.
type Step struct {
	// Phases is a collection of declarative definitions of objects.
	// Phases will be executed in parallel.
	Phases []*Phase `json:"phases"`
	// Measurements is a collection of parallel measurement calls.
	Measurements []*Measurement `json:"measurements"`
	// Module points to a CL2 module defined in a separate file.
	Module ModuleRef `json:"module"`
	// Name is an optional name for given step. If name is set the step execution
	// time will be measured and the step will be reported at the end of the test.
	// The name is ignored if the step is of type 'Module'.
	Name string `json:"name"`
}

// ModuleRef is a structure that points to a Module defined in a separate file.
type ModuleRef struct {
	// Path is the path to the filename with the module template.
	Path string `json:"path"`
	// Params specifies template parameters to be substituted inside the template.
	Params map[string]interface{} `json:"params"`
}

// Module is a structure with the definition of a CL2 module. Conceptually, a
// module is a sequence of steps.
type Module struct {
	// Steps is the list of steps composing the module. Steps are executed
	// serially.
	Steps []*Step `json:"steps"`
}

// Phase is a structure that declaratively defines state of objects.
// In a given namespace range (or cluster scope if no range is specified)
// it defines the number and the configuration of managed objects.
type Phase struct {
	// NamespaceRange defines the set of namespaces in which objects
	// should be reconciled.
	// If null, objects are assumed to be cluster scoped.
	NamespaceRange *NamespaceRange `json:"namespaceRange"`
	// ReplicasPerNamespace is a number of instances of a given object
	// to exist in each of referenced namespaces.
	ReplicasPerNamespace int32 `json:"replicasPerNamespace"`
	// TuningSet is the name of TuningSet to be used.
	TuningSet string `json:"tuningSet"`
	// ObjectBundle declaratively defines a set of objects.
	// For every specified namespace and for every required replica,
	// these objects will be reconciled in serial.
	ObjectBundle []*Object `json:"objectBundle"`
}

// Object is a structure that defines the object managed be the tests.
type Object struct {
	// Basename is a string from which names of objects will be created.
	Basename string `json:"basename"`
	// ObjectTemplatePath specifies the path to object definition.
	ObjectTemplatePath string `json:"objectTemplatePath"`
	// TemplateFillMap specifies for each placeholder what value should it be replaced with.
	TemplateFillMap map[string]interface{} `json:"templateFillMap"`
	// ListUnknownObjectOptions, if set, will result in listing objects that were
	// not created directly via ClusterLoader2 before executing Phase. The main
	// use case for that is deleting unknown objects using the Phase mechanism,
	// e.g. deleting PVs that were created via StatefulSets leveraging all Phase
	// functionalities, e.g. respecting given QPS, doing it in parallel with other
	// Phases, etc.
	ListUnknownObjectOptions *ListUnknownObjectOptions `json:"listUnknownObjectOptions"`
}

// ListUnknownObjectOptions struct specifies options for listing unknown objects.
type ListUnknownObjectOptions struct {
	LabelSelector *metav1.LabelSelector `json:"labelSelector"`
}

// NamespaceConfig defines namespace parameters.
type NamespaceConfig struct {
	// Number is a number of automanaged namespaces.
	Number int32 `json:"number,omitempty"`
	// NamePrefix is the name prefix of automanaged namespaces.
	// It's optional, if set CL will use it, otherwise generate one with random string.
	Prefix string `json:"prefix,omitempty"`
	// DeleteStaleNamespaces specifies whether or not delete stale namespaces.
	DeleteStaleNamespaces *bool `json:"deleteStaleNamespaces,omitempty"`
	// DeleteAutomanangedNamespaces specifies whether or not delete namespaces after a test.
	DeleteAutomanagedNamespaces *bool `json:"deleteAutomanagedNamespaces,omitempty"`
	// EnableExistingNamespaces enables to use pre-created namespaces in a test.
	EnableExistingNamespaces *bool `json:"enableExistingNamespaces,omitempty"`
}

// NamespaceRange specifies the range of namespaces [Min, Max].
type NamespaceRange struct {
	// Min is the lower index of namespace range.
	Min int32 `json:"min"`
	// Max is the upper index of namespace range.
	Max int32 `json:"max"`
	// Basename defines the group of selected namespaces.
	// All of the namespaces, with name "<Basename>-<i>"
	// where <i> in [Min, Max], will be selected.
	// If no Basename is specified, automanaged namespace is assumed.
	Basename *string
}

// TuningSet defines the timing of the operations.
// There is an initial delay, and then a way of choosing when to start each operation.
// It is required to have exactly one of the load structure provided.
type TuningSet struct {
	// Name by which the TuningSet will be referenced.
	Name string `json:"name"`
	// InitialDelay specifies the waiting time before starting phase execution.
	InitialDelay Duration `json:"initialDelay"`
	// QPSLoad is a definition for QPSLoad tuning set.
	QPSLoad *QPSLoad `json:"qpsLoad"`
	// RandomizedLoad is a definition for RandomizedLoad tuning set.
	RandomizedLoad *RandomizedLoad `json:"randomizedLoad"`
	// SteppedLoad is a definition for SteppedLoad tuning set.
	SteppedLoad *SteppedLoad `json:"steppedLoad"`
	// TimeLimitedLoad is a definition for TimeLimitedLoad tuning set.
	TimeLimitedLoad *TimeLimitedLoad `json:"timeLimitedLoad"`
	// RandomizedTimeLimitedLoad is a definition for RandomizedTimeLimitedLoad tuning set.
	RandomizedTimeLimitedLoad *RandomizedTimeLimitedLoad `json:"randomizedTimeLimitedLoad"`
	// ParallelismLimitedLoad is a definition for ParallelismLimitedLoad tuning set.
	ParallelismLimitedLoad *ParallelismLimitedLoad `json:"parallelismLimitedLoad"`
	// GlobalQPSLoad is a definition for GlobalQPSLoad tuning set.
	GlobalQPSLoad *GlobalQPSLoad `json:"globalQPSLoad"`
}

// MeasurementInstanceConfig is a structure that contains the Instance for wrapper measurements along with optional params.
type MeasurementInstanceConfig struct {
	// Identifier is a string that identifies a single instance of measurement within a wrapper measurement
	Identifier string `json:"identifier"`
	// Params is an optional map which is specific to the measurement instance defined above by the identifier.
	// In case the Measurement level params also contain the same configs as defined in this, then while executing that
	// particular Measurement Instance, the params defined here would be given higher priority.
	Params map[string]interface{} `json:"params"`
}

// Measurement is a structure that defines the measurement method call.
// This method call will either start or stop process of collecting specific data samples.
type Measurement struct {
	// Method is a name of a method registered in the ClusterLoader factory.
	Method string `json:"method"`
	// Params is a map of {name: value} pairs which will be passed to the measurement method - allowing for injection of arbitrary parameters to it.
	Params map[string]interface{} `json:"params"`

	// Exactly one of Identifier or Instances must be supplied.
	// Identifier is for single measurements while Instances is for wrapper measurements.
	// Identifier is a string that differentiates measurement instances of the same method.
	Identifier string `json:"identifier"`
	// MeasurementInstanceConfig contains the Identifier and Params of the measurement.
	// It shouldn't be set when Identifier is set.
	Instances []*MeasurementInstanceConfig
}

// QPSLoad starts one operation every 1/QPS seconds.
type QPSLoad struct {
	// QPS specifies requested qps.
	QPS float64 `json:"qps"`
}

// RandomizedLoad says the time between operation starts is drawn uniformly at random
// from the range [0, 2s/AverageQPS).
type RandomizedLoad struct {
	// AverageQPS specifies the expected average qps.
	AverageQPS float64 `json:"averageQps"`
}

// SteppedLoad defines a load that starts a burst of
// a given size every X seconds.
type SteppedLoad struct {
	// BurstSize specifies the qps peek.
	BurstSize int32 `json:"burstSize"`
	// StepDelay specifies the interval between peeks.
	StepDelay Duration `json:"stepDelay"`
}

// TimeLimitedLoad spreads the operation starts out evenly over a given amount of time.
type TimeLimitedLoad struct {
	// TimeLimit specifies the amount of time that the operations will be spread over.
	TimeLimit Duration `json:"timeLimit"`
}

// RandomizedTimeLimitedLoad makes an independent choice for each operation, choosing when
// it starts uniformly at random from the given total duration.
type RandomizedTimeLimitedLoad struct {
	// TimeLimit specifies the amount of time that the operations will be spread over.
	TimeLimit Duration `json:"timeLimit"`
}

// ParallelismLimitedLoad does the operations as quickly as possible subject to a given
// limit on the number running concurrently.
type ParallelismLimitedLoad struct {
	// ParallelismLimit specifies the limit of the parallelism for the action executions.
	ParallelismLimit int32 `json:"parallelismLimit"`
}

// GlobalQPSLoad defines a uniform load with a given QPS and Burst.
// The rate limiter is shared across all phases using this tuning set.
type GlobalQPSLoad struct {
	// QPS defines desired average rate of actions.
	QPS float64 `json:"qps"`
	// Burst defines maxumim number of actions that can happen at the same time.
	Burst int `json:"burst"`
}

// ChaosMonkeyConfig descibes simulated component failures.
type ChaosMonkeyConfig struct {
	// NodeFailure is a config for simulated node failures.
	NodeFailure *NodeFailureConfig `json:"nodeFailure"`
	// ExcludedNodes is a config for excluding certain nodes from failure.
	ExcludedNodes sets.String `json:"excludedNodes"`
}

// NodeFailureConfig describes simulated node failures.
type NodeFailureConfig struct {
	// FailureRate is a percentage of all nodes that could fail simultinously.
	FailureRate float64 `json:"failureRate"`
	// Interval is time between node failures.
	Interval Duration `json:"interval"`
	// JitterFactor is factor used to jitter node failures.
	// Node will be killed between [Interval, Interval + (1.0 + JitterFactor)].
	JitterFactor float64 `json:"jitterFactor"`
	// SimulatedDowntime is a duration between node is killed and recreated.
	SimulatedDowntime Duration `json:"simulatedDowntime"`
}

// Duration is time.Duration that uses string format (e.g. 1h2m3s) for marshaling.
type Duration time.Duration
