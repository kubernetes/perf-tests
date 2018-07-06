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
)

// Config is a structure that represents configuration
// for a single test scenario.
type Config struct {
	// AutomanagedNamespaces is a number of automanaged namespaces.
	AutomanagedNamespaces int32 `json: automanagedNamespaces`
	// Steps is a sequence of test steps executed in serial.
	Steps []Step `json: steps`
	// TuningSets is a collection of tuning sets that can be used by steps.
	TuningSets []TuningSet `json: tuningSets`
}

// Step represents encapsulation of some actions. These actions could be
// object declarations or measurement usages.
// Exactly one field (Phases or Measurements) should be non-empty.
type Step struct {
	// Phases is a collection of declarative definitions of objects.
	// Phases will be executed in parallel.
	Phases []Phase `json: phases`
	// Measurements is a collection of parallel measurement calls.
	Measurements []Measurement `json: measurements`
}

// Phase is a structure that declaratively defines state of objects.
// In a given namespace range (or cluster scope if no range is specified)
// it defines the number and the configuration of managed objects.
type Phase struct {
	// NamespaceRange defines the set of namespaces in which objects
	// should be reconciled.
	// If null, objects are assumed to be cluster scoped.
	NamespaceRange *NamespaceRange `json: namespaceRange`
	// ReplicasPerNamespace is a number of instances of a given object
	// to exist in each of referenced namespaces.
	ReplicasPerNamespace int32 `json: replicasPerNamespace`
	// TuningSet is the name of TuningSet to be used.
	TuningSet string `json: tuningSet`
	// ObjectBundle declaratively defines a set of objects.
	// For every specified namespace and for every required replica,
	// these objects will be reconciled in serial.
	ObjectBundle []Object `json: objectBundle`
}

// Object is a structure that defines the object managed be the tests.
type Object struct {
	// TODO(krzysied): possibly ObjectType will be removed. All data can be
	// acquired from the template.
	// ObjectType is a type for a given object.
	ObjectType ObjectType `json: objectType`
	// Basename is a string from which names of objects will be created.
	Basename string `json: basename`
	// ObjectTemplatePath specifies the path to object definition.
	ObjectTemplatePath string `json: objectTemplatePath`
}

// NamespaceRange specifies the range of namespaces [Min, Max].
type NamespaceRange struct {
	// Min is the lower index of namespace range.
	Min int32 `json: min`
	// Min is the upper index of namespace range.
	Max int32 `json: max`
	// Basename defines the group of selected namespaces.
	// All of the namespaces, with name "<Basename>-<i>"
	// where <i> in [Min, Max], will be selected.
	// If no Basename is specified, automanaged namespace is assumed.
	Basename *string
}

// ObjectType contains a specification for api and kind of the object.
type ObjectType struct {
	// APIGroup defines the api group.
	APIGroup string `json: apiGroup`
	// APIVersion specifies the api version.
	APIVersion string `json: apiVersion`
	// Kind specifies the kubernetes kind of object.
	Kind string `json: kind`
}

// TuningSet defines the specific parameterization for the simulated load limit.
// It is required to have exactly one of the load structure provided.
type TuningSet struct {
	// Name by which the TuningSet will be referenced.
	Name string `json: name`
	// InitialDelay specifies the waiting time before starting phase execution.
	InitialDelay time.Duration `json: initialDelay`
	// QpsLoad is a definition for QpsLoad tuning set.
	QpsLoad *QpsLoad `json: qpsLoad`
	// RandomizedLoad is a definition for RandomizedLoad tuning set.
	RandomizedLoad *RandomizedLoad `json: randomizedLoad`
	// SteppedLoad is a definition for SteppedLoad tuning set.
	SteppedLoad *SteppedLoad `json: steppedLoad`
}

// Measurement is a structure that defines the measurement method call.
// This method call will either start or stop process of collecting specific data samples.
type Measurement struct {
	// Method is a name of a method registered in the ClusterLoader factory.
	Method string
}

// QpsLoad defines a uniform load with a given QPS.
type QpsLoad struct {
	// Qps specifies requested qps.
	Qps float64 `json: qps`
}

// RandomizedLoad defines a load that is spread randomly
// across a given total time.
type RandomizedLoad struct {
	// AverageQps specifies the expected average qps.
	AverageQps float64 `json: averageQps`
}

// SteppedLoad defines a load that generates a burst of
// a given size every X seconds.
type SteppedLoad struct {
	// BurstSize specifies the qps peek.
	BurstSize int32 `json: burstSize`
	// StepDelay specifies the interval between peeks.
	StepDelay time.Duration `json: stepDelay`
}
