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

package api

import (
	"fmt"

	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

// Validate checks and verifies the configuration parameters.
func (conf *Config) Validate() *errors.ErrorList {
	// TODO(#1636): Validate other aspects of the api
	return conf.Namespace.Validate()
}

// Validate checks and verifies the namespace parameters.
func (ns *NamespaceConfig) Validate() *errors.ErrorList {
	errList := errors.NewErrorList()
	if ns.Number <= 0 {
		errList.Append(fmt.Errorf("number of namespaces: %d was less than 1", ns.Number))
	}

	if errList.IsEmpty() {
		return nil
	}

	return errList
}
