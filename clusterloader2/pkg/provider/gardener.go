/*
Copyright 2024 The Kubernetes Authors.

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
	"fmt"

	clientset "k8s.io/client-go/kubernetes"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

const (
	prometheusHost     = "PROMETHEUS_HOST"
	prometheusUsername = "PROMETHEUS_USERNAME"
	prometheusPassword = "PROMETHEUS_PASSWORD"
)

type GardenerProvider struct {
	features           Features
	PrometheusHost     string
	PrometheusUsername string
	PrometheusPassword string
}

func NewGardenerProvider(config map[string]string) *GardenerProvider {

	return &GardenerProvider{
		features: Features{
			SupportProbe:                        true,
			SupportSSHToMaster:                  false,
			SupportImagePreload:                 true,
			SupportEnablePrometheusServer:       false,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: false,
			SupportMetricsServerMetrics:         true,
			SupportResourceUsageMetering:        true,
			ShouldScrapeKubeProxy:               true,
		},
		PrometheusHost:     config[prometheusHost],
		PrometheusUsername: config[prometheusUsername],
		PrometheusPassword: config[prometheusPassword],
	}
}

func (p *GardenerProvider) Name() string {
	return GardenerName
}

func (p *GardenerProvider) Features() *Features {
	return &p.features
}

func (p *GardenerProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *GardenerProvider) GetConfig() Config {
	return Config{}
}

func (p *GardenerProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// TODO(#1693): To maintain compatibility with the use of SSH to scrape measurements,
	// we can SSH to localhost then run `docker exec -t <masterNodeContainerID> <cmd>`.
	return "", "", 0, fmt.Errorf("gardener: ssh not allowed")
}

func (p *GardenerProvider) Metadata(client clientset.Interface) (map[string]string, error) {
	return nil, nil
}

func (p *GardenerProvider) GetManagedPrometheusClient() (prom.Client, error) {
	if p.PrometheusHost == "" || p.PrometheusUsername == "" || p.PrometheusPassword == "" {
		return nil, ErrNoManagedPrometheus
	}
	return prom.NewGardenerManagedPrometheusClient(p.PrometheusHost, p.PrometheusUsername, p.PrometheusPassword)
}
