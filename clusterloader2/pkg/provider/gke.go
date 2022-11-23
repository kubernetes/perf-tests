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
	sshutil "k8s.io/kubernetes/test/e2e/framework/ssh"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

type GKEProvider struct {
	features Features
}

func NewGKEProvider(_ map[string]string) Provider {
	return &GKEProvider{
		features: Features{
			SupportProbe:                        true,
			SupportImagePreload:                 true,
			SupportSnapshotPrometheusDisk:       true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportNodeKiller:                   true,
			SupportResourceUsageMetering:        true,
			ShouldPrometheusScrapeApiserverOnly: true,
			ShouldScrapeKubeProxy:               false,
			SupportKubeStateMetrics:             true,
		},
	}
}

func (p *GKEProvider) Name() string {
	return GKEName
}

func (p *GKEProvider) Features() *Features {
	return &p.features
}

func (p *GKEProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *GKEProvider) GetConfig() Config {
	return Config{}
}

func (p *GKEProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// gke provider takes ssh key from GCE_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "gke")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *GKEProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *GKEProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return prom.NewGCPManagedPrometheusClient()
}
