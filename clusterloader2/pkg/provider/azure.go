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
	clientset "k8s.io/client-go/kubernetes"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

type AzureProvider struct {
	features Features
}

func NewAzureProvider(_ map[string]string) Provider {
	return &AzureProvider{
		features: Features{
			SupportProbe:                        true,
			SupportImagePreload:                 true,
			SupportGrabMetricsFromKubelets:      true,
			SupportSnapshotPrometheusDisk:       true,
			SupportEnablePrometheusServer:       true,
			SupportResourceUsageMetering:        true,
			ShouldPrometheusScrapeApiserverOnly: true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *AzureProvider) Name() string {
	return AzureName
}

func (p *AzureProvider) Features() *Features {
	return &p.features
}

func (p *AzureProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *AzureProvider) GetConfig() Config {
	return Config{}
}

func (p *AzureProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	return runSSHCommand(cmd, host)
}

func (p *AzureProvider) Metadata(_ clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *AzureProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return nil, ErrNoManagedPrometheus
}
