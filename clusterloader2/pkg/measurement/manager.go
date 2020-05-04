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
	"sync"

	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
)

// measurementManager manages all measurement executions.
type measurementManager struct {
	clusterFramework    *framework.Framework
	clusterLoaderConfig *config.ClusterLoaderConfig
	prometheusFramework *framework.Framework
	templateProvider    *config.TemplateProvider

	lock sync.Mutex
	// map from method type and identifier to measurement instance.
	measurements map[string]map[string]Measurement
	summaries    []Summary
}

// Manager provides the interface for measurementManager
type Manager interface {
	Execute(methodName string, identifier string, params map[string]interface{}) error
	GetSummaries() []Summary
	Dispose()
}

// CreateManager creates new instance of measurementManager.
func CreateManager(clusterFramework, prometheusFramework *framework.Framework, templateProvider *config.TemplateProvider, config *config.ClusterLoaderConfig) Manager {
	return &measurementManager{
		clusterFramework:    clusterFramework,
		clusterLoaderConfig: config,
		prometheusFramework: prometheusFramework,
		templateProvider:    templateProvider,
		measurements:        make(map[string]map[string]Measurement),
		summaries:           make([]Summary, 0),
	}
}

// Execute executes measurement based on provided identifier, methodName and params.
func (mm *measurementManager) Execute(methodName string, identifier string, params map[string]interface{}) error {
	measurementInstance, err := mm.getMeasurementInstance(methodName, identifier)
	if err != nil {
		return err
	}
	config := &Config{
		ClusterFramework:    mm.clusterFramework,
		PrometheusFramework: mm.prometheusFramework,
		Params:              params,
		TemplateProvider:    mm.templateProvider,
		Identifier:          identifier,
		CloudProvider:       mm.clusterLoaderConfig.ClusterConfig.Provider,
		ClusterLoaderConfig: mm.clusterLoaderConfig,
	}
	summaries, err := measurementInstance.Execute(config)
	mm.summaries = append(mm.summaries, summaries...)
	return err
}

// GetSummaries returns collected summaries.
func (mm *measurementManager) GetSummaries() []Summary {
	return mm.summaries
}

// Dispose disposes measurement instances.
func (mm *measurementManager) Dispose() {
	for _, instances := range mm.measurements {
		for _, measurement := range instances {
			measurement.Dispose()
		}
	}
}

func (mm *measurementManager) getMeasurementInstance(methodName string, identifier string) (Measurement, error) {
	mm.lock.Lock()
	defer mm.lock.Unlock()
	if _, exists := mm.measurements[methodName]; !exists {
		mm.measurements[methodName] = make(map[string]Measurement)
	}
	if _, exists := mm.measurements[methodName][identifier]; !exists {
		measurementInstance, err := factory.createMeasurement(methodName)
		if err != nil {
			return nil, err
		}
		mm.measurements[methodName][identifier] = measurementInstance
	}
	return mm.measurements[methodName][identifier], nil
}
