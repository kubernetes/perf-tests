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
)

type LocalProvider struct {
	features Features
}

func NewLocalProvider(_ map[string]string) Provider {
	return &LocalProvider{
		features: Features{
			SupportProbe:                        true,
			SupportSSHToMaster:                  true,
			SupportImagePreload:                 true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportMetricsServerMetrics:         true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *LocalProvider) Name() string {
	return LocalName
}

func (p *LocalProvider) Features() *Features {
	return &p.features
}

func (p *LocalProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *LocalProvider) GetConfig() Config {
	return Config{}
}

func (p *LocalProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// local provider takes ssh key from LOCAL_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "local")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *LocalProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}
