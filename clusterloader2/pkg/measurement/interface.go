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

package measurement

import (
	"time"

	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

// Config provides client and parameters required for the measurement execution.
type Config struct {
	// ClusterFramework returns cluster framework.
	ClusterFramework *framework.Framework
	// PrometheusFramework returns prometheus framework.
	PrometheusFramework *framework.Framework
	// Params is a map of {name: value} pairs enabling for injection of arbitrary config
	// into the Execute method.
	Params map[string]interface{}
	// TemplateProvider provides templated objects.
	TemplateProvider    *config.TemplateProvider
	ClusterLoaderConfig *config.ClusterLoaderConfig

	// Identifier identifies this instance of measurement.
	Identifier    string
	CloudProvider provider.Provider
}

// Measurement is an common interface for all measurements methods. It should be implemented by the user to
// allow his/her measurement method to be registered in the measurement factory.
// See https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/design.md for reference.
type Measurement interface {
	Execute(config *Config) ([]Summary, error)
	Dispose()
	String() string
}

type createMeasurementFunc func() Measurement

// Summary represenst result of specific measurement.
type Summary interface {
	SummaryName() string
	SummaryExt() string
	SummaryTime() time.Time
	SummaryContent() string
}
