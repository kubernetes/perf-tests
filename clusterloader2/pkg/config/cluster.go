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
	"time"

	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

// ClusterLoaderConfig represents all single test run parameters used by CLusterLoader.
type ClusterLoaderConfig struct {
	ClusterConfig     ClusterConfig
	ReportDir         string
	ExecServiceConfig ExecServiceConfig
	ModifierConfig    ModifierConfig
	PrometheusConfig  PrometheusConfig
	// OverridePaths defines what override files should be applied
	// globally to the config specified by the ConfigPath for each TestScenario.
	OverridePaths []string `json:"overridePaths"`
}

// ClusterConfig is a structure that represents cluster description.
type ClusterConfig struct {
	KubeConfigPath      string
	RunFromCluster      bool
	Nodes               int
	Provider            provider.Provider
	EtcdCertificatePath string
	EtcdKeyPath         string
	EtcdInsecurePort    int
	MasterIPs           []string
	MasterInternalIPs   []string
	MasterName          string
	// Deprecated: use NamespaceConfig.DeleteStaleNamespaces instead.
	DeleteStaleNamespaces bool
	// TODO(#1696): Clean up after removing automanagedNamespaces
	DeleteAutomanagedNamespaces bool
	// APIServerPprofByClientEnabled determines whether kube-apiserver pprof endpoint can be accessed
	// using kubernetes client. If false, clusterloader will avoid collecting kube-apiserver profiles.
	APIServerPprofByClientEnabled bool
	KubeletPort                   int
	K8SClientsNumber              int
	SkipClusterVerification       bool
}

// ExecServiceConfig represents all flags used by service config.
type ExecServiceConfig struct {
	// Determines if service config should be enabled.
	Enable bool
	// Sets path to the deployment definition.
	DeploymentYaml string
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
	TearDownServer               bool
	EnableServer                 bool
	EnablePushgateway            bool
	ScrapeEtcd                   bool
	ScrapeNodeExporter           bool
	ScrapeWindowsNodeExporter    bool
	ScrapeKubelets               bool
	ScrapeMasterKubelets         bool
	ScrapeKubeProxy              bool
	ScrapeKubeStateMetrics       bool
	ScrapeMetricsServerMetrics   bool
	ScrapeNodeLocalDNS           bool
	ScrapeAnet                   bool
	ScrapeCiliumOperator         bool
	APIServerScrapePort          int
	SnapshotProject              string
	ManifestPath                 string
	CoreManifests                string
	DefaultServiceMonitors       string
	KubeStateMetricsManifests    string
	MasterIPServiceMonitors      string
	MetricsServerManifests       string
	NodeExporterPod              string
	WindowsNodeExporterManifests string
	PushgatewayManifests         string
	StorageClassProvisioner      string
	StorageClassVolumeType       string
	ReadyTimeout                 time.Duration
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
