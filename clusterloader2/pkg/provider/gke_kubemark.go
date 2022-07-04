/*
Copyright 2022 The Kubernetes Authors.

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
	"k8s.io/klog"
	sshutil "k8s.io/kubernetes/test/e2e/framework/ssh"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

type GKEKubemarkProvider struct {
	features Features
	config   Config
}

func NewGKEKubemarkProvider(config map[string]string) *GKEKubemarkProvider {
	supportEnablePrometheusServer := true
	if config[RootKubeConfigKey] == "" {
		klog.Warningf("no kubemark-root-kubeconfig path specified. SupportEnablePrometheusServer will be false.")
		supportEnablePrometheusServer = false
	}
	return &GKEKubemarkProvider{
		features: Features{
			IsKubemarkProvider:                  true,
			SupportEnablePrometheusServer:       supportEnablePrometheusServer,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportSnapshotPrometheusDisk:       true,
			ShouldScrapeKubeProxy:               false,
			ShouldPrometheusScrapeApiserverOnly: true,
		},
		config: config,
	}
}

func (p *GKEKubemarkProvider) Name() string {
	return GKEKubemarkName
}

func (p *GKEKubemarkProvider) Features() *Features {
	return &p.features
}

func (p *GKEKubemarkProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *GKEKubemarkProvider) GetConfig() Config {
	return p.config
}

func (p *GKEKubemarkProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// gke provider takes ssh key from GCE_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "gke")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *GKEKubemarkProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *GKEKubemarkProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return prom.NewGCPManagedPrometheusClient()
}
