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
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/ticker"
)

type simpleContext struct {
	framework        *framework.Framework
	state            *state.NamespacesState
	templateProvider *config.TemplateProvider
	tickerFactory    ticker.TickerFactory
}

func createSimpleContext(f *framework.Framework, s *state.NamespacesState) Context {
	return &simpleContext{
		framework:        f,
		state:            s,
		templateProvider: config.NewTemplateProvider(),
		tickerFactory:    ticker.NewTickerFactory(),
	}
}

// GetFramework returns framework.
func (sc *simpleContext) GetFramework() *framework.Framework {
	return sc.framework
}

// GetState returns current test state.
func (sc *simpleContext) GetState() *state.NamespacesState {
	return sc.state
}

// GetTemplateProvider returns template provider.
func (sc *simpleContext) GetTemplateProvider() *config.TemplateProvider {
	return sc.templateProvider
}

// GetTickerFactory returns ticker factory.
func (sc *simpleContext) GetTickerFactory() ticker.TickerFactory {
	return sc.tickerFactory
}
