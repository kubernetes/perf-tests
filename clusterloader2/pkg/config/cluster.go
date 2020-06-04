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

import (
	"k8s.io/perf-tests/clusterloader2/api"
)

// ClusterLoaderConfig represents all single test run parameters used by CLusterLoader.
type ClusterLoaderConfig struct {
	ClusterConfig     ClusterConfig
	ReportDir         string
	EnableExecService bool
	TestScenario      api.TestScenario
	ModifierConfig    ModifierConfig
	PrometheusConfig  PrometheusConfig
}

// ClusterConfig is a structure that represents cluster description.
type ClusterConfig struct {
	KubeConfigPath             string
	RunFromCluster             bool
	Nodes                      int
	Provider                   string
	EtcdCertificatePath        string
	EtcdKeyPath                string
	EtcdInsecurePort           int
	MasterIPs                  []string
	MasterInternalIPs          []string
	MasterName                 string
	KubemarkRootKubeConfigPath string
	// Deprecated: use NamespaceConfig.DeleteStaleNamespaces instead.
	DeleteStaleNamespaces bool
	// Deprecated: use NamespaceConfig.DeleteAutomanagedNamespaces instead.
	DeleteAutomanagedNamespaces bool
	// SSHToMasterSupported determines whether SSH access to master machines is possible.
	// If false (impossible for many  providers), ClusterLoader will skip operations requiring it.
	SSHToMasterSupported bool
	// APIServerPprofByClientEnabled determines whether kube-apiserver pprof endpoint can be accessed
	// using kubernetes client. If false, clusterloader will avoid collecting kube-apiserver profiles.
	APIServerPprofByClientEnabled bool
	KubeletPort                   int
}

// ModifierConfig represent all flags used by test modification
type ModifierConfig struct {
	// A list of overwrites applied to each test config
	OverwriteTestConfig []string
	// A list of names of steps that should be ignored when executing test run
	SkipSteps []string
}

// PrometheusConfig represents all flags used by prometheus.
type PrometheusConfig struct {
	EnableServer            bool
	TearDownServer          bool
	ScrapeEtcd              bool
	ScrapeNodeExporter      bool
	ScrapeKubelets          bool
	ScrapeKubeProxy         bool
	SnapshotProject         string
	ManifestPath            string
	CoreManifests           string
	DefaultServiceMonitors  string
	MasterIPServiceMonitors string
	NodeExporterPod         string
}

// GetMasterIP returns the first master ip, added for backward compatibility.
// TODO(mmatt): Remove this method once all the codebase is migrated to support multiple masters.
func (c *ClusterConfig) GetMasterIP() string {
	if len(c.MasterIPs) > 0 {
		return c.MasterIPs[0]
	}
	return ""
}

// GetMasterInternalIP returns the first internal master ip, added for backward compatibility.
// TODO(mmatt): Remove this method once all the codebase is migrated to support multiple masters.
func (c *ClusterConfig) GetMasterInternalIP() string {
	if len(c.MasterInternalIPs) > 0 {
		return c.MasterInternalIPs[0]
	}
	return ""
}
