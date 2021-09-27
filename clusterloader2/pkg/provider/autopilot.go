/*
Copyright 2021 The Kubernetes Authors.

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
	sshutil "k8s.io/kubernetes/pkg/ssh"
)

type AutopilotProvider struct {
	features Features
}

func NewAutopilotProvider(_ map[string]string) Provider {
	return &AutopilotProvider{
		features: Features{
			SupportProbe:                        false,
			SupportImagePreload:                 true,
			SupportSnapshotPrometheusDisk:       true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportNodeKiller:                   false,
			ShouldPrometheusScrapeApiserverOnly: true,
			ShouldScrapeKubeProxy:               false,
		},
	}
}

func (p *AutopilotProvider) Name() string {
	return AutopilotName
}

func (p *AutopilotProvider) Features() *Features {
	return &p.features
}

func (p *AutopilotProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *AutopilotProvider) GetConfig() Config {
	return Config{}
}

func (p *AutopilotProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	signer, err := sshSignerFromKeyFile("GCE_SSH_KEY", "google_compute_engine")
	if err != nil {
		return "", "", 0, err
	}
	user := defaultSSHUser()
	return sshutil.RunSSHCommand(cmd, user, host, signer)
}

func (p *AutopilotProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}
