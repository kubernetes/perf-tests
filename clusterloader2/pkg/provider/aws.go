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

type AWSProvider struct {
	features Features
}

func NewAWSProvider(_ map[string]string) Provider {
	return &AWSProvider{
		features: Features{
			SupportProbe:                        true,
			SupportImagePreload:                 true,
			SupportSSHToMaster:                  true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportResourceUsageMetering:        true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *AWSProvider) Name() string {
	return AWSName
}

func (p *AWSProvider) Features() *Features {
	return &p.features
}

func (p *AWSProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *AWSProvider) GetConfig() Config {
	return Config{}
}

func (p *AWSProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// aws provider takes ssh key from AWS_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "aws")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *AWSProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *AWSProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return nil, ErrNoManagedPrometheus
}
