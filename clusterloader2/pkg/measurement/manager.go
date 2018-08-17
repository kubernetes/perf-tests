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

	clientset "k8s.io/client-go/kubernetes"
)

// MeasurementManager manages all measurement executions.
type MeasurementManager struct {
	clientSet clientset.Interface

	lock sync.Mutex
	// map from method type and identifier to measurement instance.
	measurements map[string]map[string]Measurement
}

// CreateMeasurementManager creates new instance of MeasurementManager.
func CreateMeasurementManager(clientSet clientset.Interface) *MeasurementManager {
	return &MeasurementManager{
		clientSet:    clientSet,
		measurements: make(map[string]map[string]Measurement),
	}
}

// Execute executes measurement based on provided identifier, methodName and params.
func (mm *MeasurementManager) Execute(methodName string, identifier string, params map[string]interface{}) ([]Summary, error) {
	measurementInstance, err := mm.getMeasurementInstance(methodName, identifier)
	if err != nil {
		return nil, err
	}
	config := &MeasurementConfig{
		ClientSet: mm.clientSet,
		Params:    params,
	}
	return measurementInstance.Execute(config)
}

func (mm *MeasurementManager) getMeasurementInstance(methodName string, identifier string) (Measurement, error) {
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
