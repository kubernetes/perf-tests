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

package test

import (
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
)

// CreatContextFunc a type for function that creates Context based on given framework client and state.
type CreatContextFunc func(f *framework.Framework, s *state.NamespacesState) Context

// OperationType is a type of operation to be performed on an object.
type OperationType int

const (
	// CREATE_OBJECT is create object operation.
	CREATE_OBJECT = OperationType(0)
	// UPDATE_OBJECT is update object operation.
	UPDATE_OBJECT = OperationType(1)
	// DELETE_OBJECT is delete object operation.
	DELETE_OBJECT = OperationType(2)
)

// Context is an interface for test context.
// Test context provides framework client and cluster state.
type Context interface {
	GetFramework() *framework.Framework
	GetState() *state.NamespacesState
}

// TestExecutor is an interface for test executing object.
type TestExecutor interface {
	ExecuteTest(ctx Context, conf *api.Config) []error
	ExecuteStep(ctx Context, step *api.Step) []error
	ExecutePhase(ctx Context, phase *api.Phase) []error
	ExecuteObject(ctx Context, object *api.Object, namespace string, replicaIndex int32, operation OperationType) []error
}
