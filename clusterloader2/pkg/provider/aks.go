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
)

type AKSProvider struct {
	features Features
}

func NewAKSProvider(_ map[string]string) Provider {
	return &AKSProvider{
		features: Features{
			SupportProbe:                        true,
			SupportImagePreload:                 true,
			SupportGrabMetricsFromKubelets:      true,
			SupportSnapshotPrometheusDisk:       true,
			SupportEnablePrometheusServer:       true,
			ShouldPrometheusScrapeApiserverOnly: true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *AKSProvider) Name() string {
	return AKSName
}

func (p *AKSProvider) Features() *Features {
	return &p.features
}

func (p *AKSProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *AKSProvider) GetConfig() Config {
	return Config{}
}

func (p *AKSProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	return runSSHCommand(cmd, host)
}

func (p *AKSProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}
