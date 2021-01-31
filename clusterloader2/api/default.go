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

	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// SetDefaults set the default configuration parameters.
func (conf *Config) SetDefaults() {
	conf.Namespace.SetDefaults()

	// TODO(#1696): Clean up after removing automanagedNamespaces
	if conf.Namespace.Number == 1 && conf.AutomanagedNamespaces > 1 {
		conf.Namespace.Number = conf.AutomanagedNamespaces
	}
}

// SetDefaults specifies the default values for namespace parameters.
func (ns *NamespaceConfig) SetDefaults() {
	if ns.Number == 0 {
		ns.Number = 1
	}

	if ns.Prefix == "" {
		ns.Prefix = fmt.Sprintf("test-%s", util.RandomDNS1123String(6))
	}

	defaultDeleteStaleNS := false
	if ns.DeleteStaleNamespaces == nil {
		ns.DeleteStaleNamespaces = &defaultDeleteStaleNS
	}

	defaultDeleteAutoNS := true
	if ns.DeleteAutomanagedNamespaces == nil {
		ns.DeleteAutomanagedNamespaces = &defaultDeleteAutoNS
	}

	defaultEnableExistingNS := false
	if ns.EnableExistingNamespaces == nil {
		ns.EnableExistingNamespaces = &defaultEnableExistingNS
	}
}
