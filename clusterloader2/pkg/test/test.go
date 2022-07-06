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
	"fmt"
	"path/filepath"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/modifier"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
)

var (
	// CreateContext global function for creating context.
	// This function should be set by Context implementation.
	CreateContext = createSimpleContext

	// Test is a singleton for test execution object.
	// This object should be set by Executor implementation.
	Test = createSimpleExecutor()
)

// CreateTestContext creates the test context.
func CreateTestContext(
	clusterFramework *framework.Framework,
	prometheusFramework *framework.Framework,
	clusterLoaderConfig *config.ClusterLoaderConfig,
	testReporter Reporter,
	testScenario *api.TestScenario,
) (Context, *errors.ErrorList) {
	if clusterFramework == nil {
		return nil, errors.NewErrorList(fmt.Errorf("framework must be provided"))
	}
	if clusterLoaderConfig == nil {
		return nil, errors.NewErrorList(fmt.Errorf("cluster loader config must be provided"))
	}
	if CreateContext == nil {
		return nil, errors.NewErrorList(fmt.Errorf("no CreateContext function installed"))
	}

	mapping, errList := config.GetMapping(clusterLoaderConfig, testScenario.OverridePaths)
	if errList != nil {
		return nil, errList
	}

	return CreateContext(clusterLoaderConfig, clusterFramework, prometheusFramework, state.NewState(), testReporter, mapping, testScenario), errors.NewErrorList()
}

// CompileTestConfig loads the test configuration and nested modules.
func CompileTestConfig(ctx Context) (*api.Config, *errors.ErrorList) {
	if Test == nil {
		return nil, errors.NewErrorList(fmt.Errorf("no Test installed"))
	}

	clusterLoaderConfig := ctx.GetClusterLoaderConfig()
	testScenario := ctx.GetTestScenario()
	testConfigFilename := filepath.Base(testScenario.ConfigPath)
	testConfig, err := ctx.GetTemplateProvider().TemplateToConfig(testConfigFilename, ctx.GetTemplateMappingCopy())
	if err != nil {
		return nil, errors.NewErrorList(fmt.Errorf("config reading error: %v", err))
	}

	if err := modifier.NewModifier(&clusterLoaderConfig.ModifierConfig).ChangeTest(testConfig); err != nil {
		return nil, errors.NewErrorList(fmt.Errorf("config mutation error: %v", err))
	}

	steps, err := flattenModuleSteps(ctx, testConfig.Steps)
	if err != nil {
		return nil, errors.NewErrorList(
			fmt.Errorf("error flattening module steps: %w", err))
	}
	testConfig.Steps = steps

	// TODO: remove them after the deprecated command options are removed.
	clusterFramework := ctx.GetClusterFramework()
	if testConfig.Namespace.DeleteStaleNamespaces == nil {
		testConfig.Namespace.DeleteStaleNamespaces = &clusterFramework.GetClusterConfig().DeleteStaleNamespaces
	}
	if testConfig.Namespace.DeleteAutomanagedNamespaces == nil {
		testConfig.Namespace.DeleteAutomanagedNamespaces = &clusterFramework.GetClusterConfig().DeleteAutomanagedNamespaces
	}

	testConfig.SetDefaults()
	basePath := filepath.Dir(testScenario.ConfigPath)
	configValidator := api.NewConfigValidator(basePath, testConfig)
	if err := configValidator.Validate(); err != nil {
		// Returning test config which failed validation for debugging.
		return testConfig, err
	}

	return testConfig, errors.NewErrorList()
}

// RunTest runs test based on provided test configuration.
func RunTest(ctx Context) *errors.ErrorList {
	clusterLoaderConfig := ctx.GetClusterLoaderConfig()
	testConfig := ctx.GetTestConfig()

	testName := ctx.GetTestScenario().Identifier
	if testName == "" {
		testName = testConfig.Name
	}
	ctx.GetTestReporter().SetTestName(testName)

	if err := modifier.NewModifier(&clusterLoaderConfig.ModifierConfig).ChangeTest(testConfig); err != nil {
		return errors.NewErrorList(fmt.Errorf("config mutation error: %v", err))
	}

	return Test.ExecuteTest(ctx, testConfig)
}
