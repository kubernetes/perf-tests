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
	"path/filepath"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/chaos"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/tuningset"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

type simpleContext struct {
	clusterLoaderConfig *config.ClusterLoaderConfig
	clusterFramework    *framework.Framework
	prometheusFramework *framework.Framework
	testReporter        Reporter
	state               *state.State
	templateMapping     map[string]interface{}
	templateProvider    *config.TemplateProvider
	tuningSetFactory    tuningset.Factory
	measurementManager  measurement.Manager
	chaosMonkey         *chaos.Monkey
	testScenario        *api.TestScenario
	testConfig          *api.Config
}

func createSimpleContext(c *config.ClusterLoaderConfig, f, p *framework.Framework, s *state.State, testReporter Reporter, templateMapping map[string]interface{}, testScenario *api.TestScenario) Context {
	templateProvider := config.NewTemplateProvider(filepath.Dir(testScenario.ConfigPath))
	return &simpleContext{
		clusterLoaderConfig: c,
		clusterFramework:    f,
		prometheusFramework: p,
		testReporter:        testReporter,
		state:               s,
		templateMapping:     util.CloneMap(templateMapping),
		templateProvider:    templateProvider,
		tuningSetFactory:    tuningset.NewFactory(),
		measurementManager:  measurement.CreateManager(f, p, templateProvider, c),
		chaosMonkey:         chaos.NewMonkey(f.GetClientSets().GetClient(), c.ClusterConfig.Provider),
		testScenario:        testScenario,
	}
}

// GetClusterConfig return cluster config.
func (sc *simpleContext) GetClusterLoaderConfig() *config.ClusterLoaderConfig {
	return sc.clusterLoaderConfig
}

// GetFramework returns cluster framework.
func (sc *simpleContext) GetClusterFramework() *framework.Framework {
	return sc.clusterFramework
}

// GetFramework returns prometheus framework.
func (sc *simpleContext) GetPrometheusFramework() *framework.Framework {
	return sc.prometheusFramework
}

// GetTestReporter returns test items reporter.
func (sc *simpleContext) GetTestReporter() Reporter {
	return sc.testReporter
}

// GetState returns current test state.
func (sc *simpleContext) GetState() *state.State {
	return sc.state
}

// GetTemplateProvider returns template provider.
func (sc *simpleContext) GetTemplateProvider() *config.TemplateProvider {
	return sc.templateProvider
}

// GetTemplateMappingCopy returns a copy of template mapping.
func (sc *simpleContext) GetTemplateMappingCopy() map[string]interface{} {
	return util.CloneMap(sc.templateMapping)
}

// GetTickerFactory returns tuning set factory.
func (sc *simpleContext) GetFactory() tuningset.Factory {
	return sc.tuningSetFactory
}

// GetManager returns measurement manager.
func (sc *simpleContext) GetManager() measurement.Manager {
	return sc.measurementManager
}

// GetChaosMonkey returns chaos monkey.
func (sc *simpleContext) GetChaosMonkey() *chaos.Monkey {
	return sc.chaosMonkey
}

// GetTestScenario returns test scenario.
func (sc *simpleContext) GetTestScenario() *api.TestScenario {
	return sc.testScenario
}

// GetTestConfig returns test config.
func (sc *simpleContext) GetTestConfig() *api.Config {
	return sc.testConfig
}

// SetTestConfig sets test config.
func (sc *simpleContext) SetTestConfig(c *api.Config) {
	sc.testConfig = c
}
