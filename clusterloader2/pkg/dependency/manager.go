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
	"context"
	"fmt"
	"sync"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
)

// dependencyManager manages all dependency executions.
type dependencyManager struct {
	clusterFramework    *framework.Framework
	clusterLoaderConfig *config.ClusterLoaderConfig
	prometheusFramework *framework.Framework
	templateProvider    *config.TemplateProvider

	lock sync.Mutex
	// map from method type dependency instance.
	dependencies map[string]Dependency
	// list of setup dependencies in order for teardown
	setupDependencies []*dependencyInstance
}

type dependencyInstance struct {
	*api.Dependency
	dependency Dependency
}

// Manager provides the interface for dependencyManager
type Manager interface {
	// SetupDependencies sets up all dependencies defined in the config
	SetupDependencies(dependencies []*api.Dependency) *errors.ErrorList
	// TeardownDependencies tears down all previously setup dependencies in reverse order
	TeardownDependencies() *errors.ErrorList
}

// CreateManager creates new instance of dependencyManager.
func CreateManager(clusterFramework, prometheusFramework *framework.Framework, templateProvider *config.TemplateProvider, config *config.ClusterLoaderConfig) Manager {
	return &dependencyManager{
		clusterFramework:    clusterFramework,
		clusterLoaderConfig: config,
		prometheusFramework: prometheusFramework,
		templateProvider:    templateProvider,
		dependencies:        make(map[string]Dependency),
		setupDependencies:   make([]*dependencyInstance, 0),
	}
}

// SetupDependencies sets up all dependencies with timeout enforcement
func (dm *dependencyManager) SetupDependencies(dependencies []*api.Dependency) *errors.ErrorList {
	errList := errors.NewErrorList()
	for _, dep := range dependencies {
		if err := dm.setupSingleDependency(dep); err != nil {
			errList.Append(err)
		}
	}
	if !errList.IsEmpty() {
		klog.V(2).ErrorS(errList, "error setting up dependencies")
	}
	return errList
}

// TeardownDependencies tears down all dependencies in reverse order
func (dm *dependencyManager) TeardownDependencies() *errors.ErrorList {
	errList := errors.NewErrorList()
	for i := len(dm.setupDependencies) - 1; i >= 0; i-- {
		depInstance := dm.setupDependencies[i]
		if err := dm.teardownDependencyInstance(depInstance); err != nil {
			errList.Append(err)
		}
	}
	if !errList.IsEmpty() {
		klog.V(2).ErrorS(errList, "error tearing down dependencies")
	}
	return errList
}

func (dm *dependencyManager) setupSingleDependency(dep *api.Dependency) error {
	depInstance, err := dm.getDependencyInstance(dep.Method)
	if err != nil {
		return err
	}

	config := &Config{
		ClusterFramework:    dm.clusterFramework,
		PrometheusFramework: dm.prometheusFramework,
		Params:              dep.Params,
		TemplateProvider:    dm.templateProvider,
		Method:              dep.Method,
		CloudProvider:       dm.clusterLoaderConfig.ClusterConfig.Provider,
		ClusterLoaderConfig: dm.clusterLoaderConfig,
	}

	clusterVersion, err := dm.clusterFramework.GetDiscoveryClient().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to get cluster version: %w", err)
	}
	config.ClusterVersion = *clusterVersion

	// Apply timeout if specified
	if dep.Timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), dep.Timeout.ToTimeDuration())
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- depInstance.Setup(config)
		}()

		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("dependency %s setup failed: %w", dep.Name, err)
			}
		case <-ctx.Done():
			return fmt.Errorf("dependency %s setup timed out after %v", dep.Name, dep.Timeout.ToTimeDuration())
		}
	} else {
		if err := depInstance.Setup(config); err != nil {
			return fmt.Errorf("dependency %s setup failed: %w", dep.Name, err)
		}
	}

	// Track successful setup for later teardown
	dm.setupDependencies = append(dm.setupDependencies, &dependencyInstance{
		Dependency: dep,
		dependency: depInstance,
	})

	return nil
}

func (dm *dependencyManager) teardownDependencyInstance(depInstance *dependencyInstance) error {
	conf := &Config{
		ClusterFramework:    dm.clusterFramework,
		PrometheusFramework: dm.prometheusFramework,
		Params:              depInstance.Params,
		TemplateProvider:    dm.templateProvider,
		Method:              depInstance.Method,
		CloudProvider:       dm.clusterLoaderConfig.ClusterConfig.Provider,
		ClusterLoaderConfig: dm.clusterLoaderConfig,
	}

	clusterVersion, err := dm.clusterFramework.GetDiscoveryClient().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to get cluster version during teardown: %w", err)
	}
	conf.ClusterVersion = *clusterVersion

	// TODO: Add timeout support for teardown when needed
	if err := depInstance.dependency.Teardown(conf); err != nil {
		return fmt.Errorf("dependency %s teardown failed: %w", depInstance.Name, err)
	}

	return nil
}

func (dm *dependencyManager) getDependencyInstance(methodName string) (Dependency, error) {
	dm.lock.Lock()
	defer dm.lock.Unlock()
	if _, exists := dm.dependencies[methodName]; !exists {
		di, err := factory.createDependency(methodName)
		if err != nil {
			return nil, err
		}
		dm.dependencies[methodName] = di
	}
	return dm.dependencies[methodName], nil
}
