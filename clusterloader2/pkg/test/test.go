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
	"os"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

var (
	// CreateContext global function for creating context.
	// This function should be set by Context implementation.
	CreateContext = createSimpleContext

	// Test is a singleton for test execution object.
	// This object should be set by TestExecutor implementation.
	Test = createSimpleTestExecutor()
)

// RunTest runs test based on provided test configuration.
func RunTest(f *framework.Framework, clusterLoaderConfig *config.ClusterLoaderConfig, testConfig *api.Config) *util.ErrorList {
	if f == nil {
		return util.NewErrorList(fmt.Errorf("framework must be provided"))
	}
	if clusterLoaderConfig == nil {
		return util.NewErrorList(fmt.Errorf("cluster loader config must be provided"))
	}
	if testConfig == nil {
		return util.NewErrorList(fmt.Errorf("test config must be provided"))
	}
	if CreateContext == nil {
		return util.NewErrorList(fmt.Errorf("no CreateContext function installed"))
	}
	if Test == nil {
		return util.NewErrorList(fmt.Errorf("no Test installed"))
	}

	if clusterLoaderConfig.ReportDir != "" {
		if _, err := os.Stat(clusterLoaderConfig.ReportDir); err != nil {
			if !os.IsNotExist(err) {
				return util.NewErrorList(err)
			}
			if err = os.Mkdir(clusterLoaderConfig.ReportDir, 0755); err != nil {
				return util.NewErrorList(fmt.Errorf("report directory creation error: %v", err))
			}
		}
	}
	ctx := CreateContext(clusterLoaderConfig, f, state.NewNamespacesState())
	return Test.ExecuteTest(ctx, testConfig)
}
