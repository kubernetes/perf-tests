/*
Copyright 2020 The Kubernetes Authors.

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

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

// Execute executes a measurement, which can be a single measurement or a wrapper for multiple measurements.
func Execute(mm Manager, m *api.Measurement) *errors.ErrorList {
	if m.Identifier != "" {
		return executeSingleMeasurement(mm, m)
	}
	return executeWrapperMeasurement(mm, m)
}

func formatError(method, identifier string, err error) error {
	return fmt.Errorf("measurement call %s - %s error: %v", method, identifier, err)
}

func executeSingleMeasurement(mm Manager, m *api.Measurement) *errors.ErrorList {
	errList := errors.NewErrorList()
	if err := mm.Execute(m.Method, m.Identifier, m.Params); err != nil {
		errList.Append(formatError(m.Method, m.Identifier, err))
	}
	return errList
}

func executeWrapperMeasurement(mm Manager, m *api.Measurement) *errors.ErrorList {
	var wg wait.Group
	errList := errors.NewErrorList()
	for i := range m.Instances {
		identifier := m.Instances[i].Identifier
		measurementInstanceParams := m.Instances[i].Params
		// clone the measurement params
		measurementParams := make(map[string]interface{})
		for k, v := range m.Params {
			measurementParams[k] = v
		}
		// add/overwrite the measurement params with wrapper measurement params
		for paramsKey := range measurementInstanceParams {
			measurementParams[paramsKey] = measurementInstanceParams[paramsKey]
		}
		wg.Start(func() {
			if err := mm.Execute(m.Method, identifier, measurementParams); err != nil {
				errList.Append(formatError(m.Method, identifier, err))
			}
		})
	}
	wg.Wait()
	return errList
}
