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
	"fmt"
	"io"
	"net/http"
	
	"time"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// aksManagedPrometheusClient talks to the Azure Managed Prometheus.
// This only works if the cluster is hosted in AKS.
// Overview: https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/prometheus-metrics-overview.
// Query API: https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/prometheus-api-promql .
type aksManagedPrometheusClient struct {
	uri   			string
	headers    		map[string]string
}

func (mpc *aksManagedPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	req, err := http.NewRequest("GET", mpc.uri, nil)
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
    tokenRequestContext := []string{"https://prometheus.monitor.azure.com/.default"}
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    accessToken, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: tokenRequestContext})
    if err != nil {
        return "Failed to get Azure Prometheus Token", err
    }
    token := accessToken.Token
    return token, nil
}

// NewAKSManagedPrometheusClient returns instance of aksManagedPrometheusClient 
// which is configured to query Azure Managed Prometheus endpoint.
func NewAKSManagedPrometheusClient() (Client, error) {
	credential, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{})
	if err != nil {
		return nil, err
	}
	promQLToken, err := getPromQLToken(credential)
    if err != nil {
        fmt.Println("Failed to get Azure Prometheus :", err)
        return nil, err
    }
	return &aksManagedPrometheusClient{
		uri:   fmt.Sprintf("%s/api/v1/query", os.Getenv("AZURE_QUERY_ENDPOINT")),
		headers: map[string]string{
			"Authorization": "Bearer " + promQLToken,
		},
	}, nil
}

var _ Client = &aksManagedPrometheusClient{}
