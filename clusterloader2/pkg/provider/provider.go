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

package provider

import (
	"fmt"
	"strings"

	clientset "k8s.io/client-go/kubernetes"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

// InitOptions encapsulates the fields needed to init provider.
type InitOptions struct {
	// TODO(#1361) remove this and use providerConfigs.
	KubemarkRootKubeConfigPath string
	ProviderName               string
	ProviderConfigs            []string
}

// Features represents all features supported by this provider.
type Features struct {
	// Some features do not work for kubemark-like providers or have separate implementation.
	IsKubemarkProvider bool
	// SupportWindowsNodeScraping determines wheter scraping windows node in supported.
	SupportWindowsNodeScraping bool
	// SupportProbe determines whether probe is supported.
	SupportProbe bool
	// SupportImagePreload determines whether image preloading is supported.
	SupportImagePreload bool
	// SupportEnablePrometheusServer determines whether enabling prometheus server is possible.
	SupportEnablePrometheusServer bool
	// SupportSSHToMaster determines whether SSH access to master machines is possible.
	// If false (impossible for many  providers), ClusterLoader will skip operations requiring it.
	SupportSSHToMaster bool
	// SupportAccessAPIServerPprofEndpoint determines whether accessing api server pprof endpoint is possible.
	SupportAccessAPIServerPprofEndpoint bool
	// SupportSnapshotPrometheusDisk determines whether snapshot prometheus disk is supported.
	SupportSnapshotPrometheusDisk bool
	// SupportNodeKiller determines whether node killer is supported.
	SupportNodeKiller bool
	// SupportGrabMetricsFromKubelets determines whether getting metrics from kubelet is supported.
	SupportGrabMetricsFromKubelets bool
	// SupportKubeStateMetrics determines if running kube-state-metrics is supported.
	SupportKubeStateMetrics bool
	// SupportMetricsServerMetrics determines if running metrics server is supported.
	SupportMetricsServerMetrics bool
	// SupportResourceUsageMetering determines if resource usage measurement is supported.
	SupportResourceUsageMetering bool

	// ShouldPrometheusScrapeApiserverOnly determines if we should set PROMETHEUS_SCRAPE_APISERVER_ONLY by default.
	ShouldPrometheusScrapeApiserverOnly bool

	// SchedulerInsecurePortDisabled determines if kube-scheduler listens on insecure port.
	SchedulerInsecurePortDisabled bool

	// ShouldScrapeKubeProxy determines if ScrapeKubeProxy
	ShouldScrapeKubeProxy bool
}

// Config is the config of the provider.
type Config map[string]string

// RootFrameworkKubeConfigOverride returns the KubeConfig override for Root Framewor.
func (c Config) RootFrameworkKubeConfigOverride() string {
	return c[RootKubeConfigKey]
}

// Provider is the interface for
type Provider interface {
	// Name returns name of this provider. It should used only in logs.
	Name() string
	// Features returns the feature supported by this provider.
	Features() *Features

	GetConfig() Config

	// GetManagedPrometheusClient returns HTTP client for communicating with the relevant cloud provider's managed Prometheus service.
	GetManagedPrometheusClient() (prom.Client, error)

	// GetComponentProtocolAndPort returns the protocol and port for the control plane components.
	GetComponentProtocolAndPort(componentName string) (string, int, error)

	RunSSHCommand(cmd, host string) (string, string, int, error)

	// Metadata returns provider-specific test run metadata.
	Metadata(client clientset.Interface) (map[string]string, error)
}

// NewProvider creates a new provider from init options. It will return an error if provider name is not supported.
func NewProvider(initOptions *InitOptions) (Provider, error) {
	configs := parseConfigs(initOptions.ProviderConfigs)
	if initOptions.KubemarkRootKubeConfigPath != "" {
		configs[RootKubeConfigKey] = initOptions.KubemarkRootKubeConfigPath
	}
	switch initOptions.ProviderName {
	case AKSName:
		return NewAKSProvider(configs), nil
	case AWSName:
		return NewAWSProvider(configs), nil
	case AutopilotName:
		return NewAutopilotProvider(configs), nil
	case EKSName:
		return NewEKSProvider(configs), nil
	case GCEName:
		return NewGCEProvider(configs), nil
	case GKEName:
		return NewGKEProvider(configs), nil
	case GKEKubemarkName:
		return NewGKEKubemarkProvider(configs), nil
	case KCPName:
		return NewKCPProvider(configs), nil
	case KindName:
		return NewKindProvider(configs), nil
	case KubemarkName:
		return NewKubemarkProvider(configs), nil
	case LocalName:
		return NewLocalProvider(configs), nil
	case SkeletonName:
		return NewSkeletonProvider(configs), nil
	case VsphereName:
		return NewVsphereProvider(configs), nil
	default:
		return nil, fmt.Errorf("unsupported provider name: %s", initOptions.ProviderName)
	}
}

func parseConfigs(configs []string) map[string]string {
	cfgMap := map[string]string{}

	for _, c := range configs {
		pair := strings.Split(c, "=")
		if len(pair) != 2 {
			fmt.Println("error: unused provider config:", c)
		}
		cfgMap[pair[0]] = pair[1]
	}
	return cfgMap
}
