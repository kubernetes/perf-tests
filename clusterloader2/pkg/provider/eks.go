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
	sshutil "k8s.io/kubernetes/pkg/ssh"
)

type EKSProvider struct {
	features Features
}

func NewEKSProvider(_ map[string]string) Provider {
	return &EKSProvider{
		features: Features{
			SupportProbe:                        true,
			SupportImagePreload:                 true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *EKSProvider) Name() string {
	return EKSName
}

func (p *EKSProvider) Features() *Features {
	return &p.features
}

func (p *EKSProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *EKSProvider) GetConfig() Config {
	return Config{}
}

func (p *EKSProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	signer, err := sshSignerFromKeyFile("AWS_SSH_KEY", "kube_aws_rsa")
	if err != nil {
		return "", "", 0, err
	}
	user := defaultSSHUser()
	return sshutil.RunSSHCommand(cmd, user, host, signer)
}

func (p *EKSProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}
