/*
Copyright 2025 The Kubernetes Authors.

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

package dependency

import (
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"

	"k8s.io/apimachinery/pkg/version"
)

// Config provides client and parameters required for the dependency execution.
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

	// Method identifies this instance of dependency.
	Method        string
	CloudProvider provider.Provider

	// ClusterVersion contains the version of the cluster and is used to select
	// available features.
	ClusterVersion version.Info
}

// Dependency is a common interface for all dependency methods. It should be implemented by the user to
// allow dependency method to be registered in the dependency factory.
type Dependency interface {
	// Setup sets up the dependency and returns an error if setup fails.
	Setup(config *Config) error
	// Teardown tears down the dependency and returns an error if teardown fails.
	Teardown(config *Config) error
	// String returns a string representation of the dependency.
	String() string
}

type createDependencyFunc func() Dependency
