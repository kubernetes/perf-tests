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

package config

// ClusterLoaderConfig represents all flags used by CLusterLoader
type ClusterLoaderConfig struct {
	ClusterConfig  ClusterConfig `json: clusterConfig`
	ReportDir      string        `json: reportDir`
	TestConfigPath string        `json: testConfigPath`
}

// ClusterConfig is a structure that represents cluster description.
type ClusterConfig struct {
	KubeConfigPath string `json: kubeConfigPath`
	Nodes          int    `json: nodes`
}
