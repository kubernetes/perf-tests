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
	"k8s.io/klog"
	sshutil "k8s.io/kubernetes/test/e2e/framework/ssh"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

type KubemarkProvider struct {
	features Features
	config   Config
}

func NewKubemarkProvider(config map[string]string) *KubemarkProvider {
	supportEnablePrometheusServer := true
	if config[RootKubeConfigKey] == "" {
		klog.Warningf("no kubemark-root-kubeconfig path specified. SupportEnablePrometheusServer will be false.")
		supportEnablePrometheusServer = false
	}
	return &KubemarkProvider{
		features: Features{
			IsKubemarkProvider:                  true,
			SupportSSHToMaster:                  true,
			SupportEnablePrometheusServer:       supportEnablePrometheusServer,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportSnapshotPrometheusDisk:       true,
			ShouldScrapeKubeProxy:               true,
		},
		config: config,
	}
}

func (p *KubemarkProvider) Name() string {
	return KubemarkName
}

func (p *KubemarkProvider) Features() *Features {
	return &p.features
}

func (p *KubemarkProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *KubemarkProvider) GetConfig() Config {
	return p.config
}

func (p *KubemarkProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// kubemark provider takes ssh key from GCE_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "kubemark")
	return r.Stdout, r.Stderr, r.Code, err
}

// TODO(mborsz): Dump instanceIDs for master nodes (as in gce).
func (p *KubemarkProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *KubemarkProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return nil, ErrNoManagedPrometheus
}
