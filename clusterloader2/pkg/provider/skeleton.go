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

type SkeletonProvider struct {
	features Features
}

func NewSkeletonProvider(_ map[string]string) Provider {
	return &SkeletonProvider{
		features: Features{
			SupportProbe:                        true,
			SupportSSHToMaster:                  true,
			SupportImagePreload:                 true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportResourceUsageMetering:        true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *SkeletonProvider) Name() string {
	return SkeletonName
}

func (p *SkeletonProvider) Features() *Features {
	return &p.features
}

func (p *SkeletonProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *SkeletonProvider) GetConfig() Config {
	return Config{}
}

func (p *SkeletonProvider) GetRootFrameworkKubeConfigOverride() string {
	return ""
}

func (p *SkeletonProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// skeleton provider takes ssh key from KUBE_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "skeleton")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *SkeletonProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *SkeletonProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return nil, ErrNoManagedPrometheus
}
