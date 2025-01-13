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

package prom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"k8s.io/klog/v2"
)

const (
	azureQueryEndpointEnv = "AZURE_QUERY_ENDPOINT"
	tokenRequestContext   = "https://prometheus.monitor.azure.com/.default"
)

// aksManagedPrometheusClient is a client for querying Azure Managed Prometheus in an AKS managed environment.
// It contains the URI for the Azure Managed Prometheus query endpoint and necessary headers for authentication.
// Overview: https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/prometheus-metrics-overview.
// Query API: https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/prometheus-api-promql.
type aksManagedPrometheusClient struct {
	uri     string
	headers map[string]string
}

// Query creates an HTTP request to query Azure Prometheus endpoint at a specific time,
// and returns the result as a byte slice.
func (mpc *aksManagedPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", mpc.uri, nil)
	if err != nil {
		return nil, err
	}

	// Set query parameters
	q := req.URL.Query()
	q.Add("query", query)
	q.Add("time", queryTime.Format(time.RFC3339))
	req.URL.RawQuery = q.Encode()

	// Set headers
	for key, value := range mpc.headers {
		req.Header.Add(key, value)
	}

	// Execute the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read and handle the response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if statusCode := resp.StatusCode; statusCode > 299 {
		return respBody, fmt.Errorf("response failed with status code %d", statusCode)
	}
	return respBody, nil
}

func getPromQLToken(credential *azidentity.DefaultAzureCredential) (string, error) {
	tokenRequestContext := []string{tokenRequestContext}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accessToken, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: tokenRequestContext})
	if err != nil {
		return "", err
	}
	token := accessToken.Token
	return token, nil
}

// NewAKSManagedPrometheusClient returns an instance of aksManagedPrometheusClient
// which is configured to query the Azure Managed Prometheus endpoint.
func NewAKSManagedPrometheusClient() (Client, error) {
	// Check if AZURE_QUERY_ENDPOINT is set
	endpoint := os.Getenv(azureQueryEndpointEnv)
	if endpoint == "" {
		return nil, fmt.Errorf("environment variable AZURE_QUERY_ENDPOINT is not set")
	}

	credential, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{})
	if err != nil {
		return nil, err
	}
	promQLToken, err := getPromQLToken(credential)
	if err != nil {
		klog.Errorf("Failed to get Azure Prometheus: %v", err)
		return nil, err
	}
	return &aksManagedPrometheusClient{
		uri: fmt.Sprintf("%s/api/v1/query", endpoint),
		headers: map[string]string{
			"Authorization": "Bearer " + promQLToken,
		},
	}, nil
}

var _ Client = &aksManagedPrometheusClient{}
