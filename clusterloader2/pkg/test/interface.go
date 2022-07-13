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
	"time"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/chaos"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/tuningset"
)

// CreatContextFunc a type for function that creates Context based on given framework client and state.
type CreatContextFunc func(c *config.ClusterLoaderConfig, f *framework.Framework, s *state.State) Context

// OperationType is a type of operation to be performed on an object.
type OperationType int

const (
	// createObject is create object operation.
	createObject = OperationType(0)
	// patchObject is update object (using patch) operation.
	// TODO(krzysied): Figure out how to implement UPDATE_OBJECT operation.
	patchObject = OperationType(1)
	// deleteObject is delete object operation.
	deleteObject = OperationType(2)
)

// Context is an interface for test context.
// Test context provides framework client and cluster state.
type Context interface {
	GetClusterLoaderConfig() *config.ClusterLoaderConfig
	GetClusterFramework() *framework.Framework
	GetPrometheusFramework() *framework.Framework
	GetTestReporter() Reporter
	GetState() *state.State
	GetTemplateMappingCopy() map[string]interface{}
	GetTemplateProvider() *config.TemplateProvider
	GetFactory() tuningset.Factory
	GetManager() measurement.Manager
	GetChaosMonkey() *chaos.Monkey
	GetTestScenario() *api.TestScenario
	GetTestConfig() *api.Config
	SetTestConfig(*api.Config)
}

// Executor is an interface for test executing object.
type Executor interface {
	ExecuteTest(ctx Context, conf *api.Config) *errors.ErrorList
	ExecuteStep(ctx Context, step *api.Step) *errors.ErrorList
	ExecutePhase(ctx Context, phase *api.Phase) *errors.ErrorList
	ExecuteObject(ctx Context, object *api.Object, namespace string, replicaIndex int32, operation OperationType) *errors.ErrorList
}

// Reporter is an interface for reporting tests results.
type Reporter interface {
	SetTestName(name string)
	GetNumberOfFailedTestItems() int
	BeginTestSuite()
	EndTestSuite()
	ReportTestStepFinish(duration time.Duration, stepName string, errList *errors.ErrorList)
	ReportTestStep(result *StepResult)
	ReportTestFinish(duration time.Duration, testConfigPath string, errList *errors.ErrorList)
}
