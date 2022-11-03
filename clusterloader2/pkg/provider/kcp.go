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

type KCPProvider struct {
	features Features
}

func NewKCPProvider(_ map[string]string) Provider {
	return &KCPProvider{
		features: Features{
			SupportProbe:                        false,
			SupportSSHToMaster:                  false,
			SupportImagePreload:                 false,
			SupportEnablePrometheusServer:       false,
			SupportGrabMetricsFromKubelets:      false,
			SupportAccessAPIServerPprofEndpoint: false,
			SupportResourceUsageMetering:        true,
			ShouldScrapeKubeProxy:               false,
		},
	}
}

func (p *KCPProvider) Name() string {
	return KCPName
}

func (p *KCPProvider) Features() *Features {
	return &p.features
}

func (p *KCPProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *KCPProvider) GetConfig() Config {
	return Config{}
}

func (p *KCPProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	return "", "", 0, nil
}

func (p *KCPProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *KCPProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return nil, ErrNoManagedPrometheus
}
