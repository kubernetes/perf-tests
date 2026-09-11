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

package prom

import (
	"context"
	"time"

	clientset "k8s.io/client-go/kubernetes"
)

const (
	// DefaultServiceName is the Service created by --enable-prometheus-server.
	DefaultServiceName = "prometheus-k8s"
	// DefaultProxyScheme matches kube-prometheus (http Service port).
	DefaultProxyScheme = "http"
)

var (
	inClusterServiceName = DefaultServiceName
	inClusterProxyScheme = DefaultProxyScheme
)

// ConfigureInClusterProxy sets the Service name and scheme used for apiserver
// proxy PromQL. Empty values reset to defaults.
func ConfigureInClusterProxy(scheme, serviceName string) {
	if scheme == "" {
		inClusterProxyScheme = DefaultProxyScheme
	} else {
		inClusterProxyScheme = scheme
	}
	if serviceName == "" {
		inClusterServiceName = DefaultServiceName
	} else {
		inClusterServiceName = serviceName
	}
}

// InClusterServiceName returns the monitoring-namespace Service used for PromQL.
func InClusterServiceName() string {
	return inClusterServiceName
}

// InClusterProxyScheme returns the apiserver service-proxy scheme (http or https).
func InClusterProxyScheme() string {
	return inClusterProxyScheme
}

// inClusterPrometheusClient talks to the Prometheus instance deployed in the test cluster.
type inClusterPrometheusClient struct {
	client      clientset.Interface
	scheme      string
	serviceName string
}

func (icpc *inClusterPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	params := map[string]string{
		"query": query,
		"time":  queryTime.Format(time.RFC3339),
	}
	return icpc.client.CoreV1().
		Services("monitoring").
		ProxyGet(icpc.scheme, icpc.serviceName, "9090", "api/v1/query", params).
		DoRaw(context.TODO())
}

func NewInClusterPrometheusClient(c clientset.Interface) Client {
	return &inClusterPrometheusClient{
		client:      c,
		scheme:      InClusterProxyScheme(),
		serviceName: InClusterServiceName(),
	}
}

var _ Client = &inClusterPrometheusClient{}
