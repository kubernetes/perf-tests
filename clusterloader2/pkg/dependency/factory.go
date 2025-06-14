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
	"fmt"
	"sync"

	"k8s.io/perf-tests/clusterloader2/api"
)

// factory is a default global factory instance.
var factory = newDependencyFactory()

func newDependencyFactory() *dependencyFactory {
	return &dependencyFactory{
		createFuncs: make(map[string]createDependencyFunc),
	}
}

// dependencyFactory is a factory that creates dependency instances.
type dependencyFactory struct {
	lock        sync.RWMutex
	createFuncs map[string]createDependencyFunc
}

func (df *dependencyFactory) register(methodName string, createFunc createDependencyFunc) error {
	df.lock.Lock()
	defer df.lock.Unlock()
	_, exists := df.createFuncs[methodName]
	if exists {
		return fmt.Errorf("dependency with method %v already exists", methodName)
	}
	df.createFuncs[methodName] = createFunc
	api.RegisteredDependencies[methodName] = true
	return nil
}

func (df *dependencyFactory) createDependency(methodName string) (Dependency, error) {
	df.lock.RLock()
	defer df.lock.RUnlock()
	createFunc, exists := df.createFuncs[methodName]
	if !exists {
		return nil, fmt.Errorf("unknown dependency method %s", methodName)
	}
	return createFunc(), nil
}

// Register registers create dependency function in dependency factory.
func Register(methodName string, createFunc createDependencyFunc) error {
	return factory.register(methodName, createFunc)
}
