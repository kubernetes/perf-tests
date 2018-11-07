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
	"fmt"
	"sync"
)

// Factory is a default global factory instance.
var factory = newMeasurementFactory()

func newMeasurementFactory() *measurementFactory {
	return &measurementFactory{
		createFuncs: make(map[string]createMeasurementFunc),
	}
}

// measurementFactory is a factory that creates measurement instances.
type measurementFactory struct {
	lock        sync.RWMutex
	createFuncs map[string]createMeasurementFunc
}

func (mc *measurementFactory) register(methodName string, createFunc createMeasurementFunc) error {
	mc.lock.Lock()
	defer mc.lock.Unlock()
	_, exists := mc.createFuncs[methodName]
	if exists {
		return fmt.Errorf("measurement with method %v already exists", methodName)
	}
	mc.createFuncs[methodName] = createFunc
	return nil
}

func (mc *measurementFactory) createMeasurement(methodName string) (Measurement, error) {
	mc.lock.RLock()
	defer mc.lock.RUnlock()
	createFunc, exists := mc.createFuncs[methodName]
	if !exists {
		return nil, fmt.Errorf("unknown measurement method %s", methodName)
	}
	return createFunc(), nil
}

// Register registers create measurement function in measurement factory.
func Register(methodName string, createFunc createMeasurementFunc) error {
	return factory.register(methodName, createFunc)
}

// CreateMeasurement creates measurement instance.
func CreateMeasurement(methodName string) (Measurement, error) {
	return factory.createMeasurement(methodName)
}
