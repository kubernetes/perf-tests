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

// inClusterPrometheusClient talks to the Prometheus instance deployed in the test cluster.
type inClusterPrometheusClient struct {
	client clientset.Interface
}

func (icpc *inClusterPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	params := map[string]string{
		"query": query,
		"time":  queryTime.Format(time.RFC3339),
	}
	return icpc.client.CoreV1().
		Services("monitoring").
		ProxyGet("http", "prometheus-k8s", "9090", "api/v1/query", params).
		DoRaw(context.TODO())
}

func NewInClusterPrometheusClient(c clientset.Interface) Client {
	return &inClusterPrometheusClient{client: c}
}

var _ Client = &inClusterPrometheusClient{}
