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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type gardenerManagedPrometheusClient struct {
	client *http.Client
	uri    string
}

func (mpc *gardenerManagedPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	params := url.Values{}
	params.Add("query", query)
	params.Add("time", queryTime.Format(time.RFC3339))
	res, err := mpc.client.Get(mpc.uri + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if statusCode := res.StatusCode; statusCode > 299 {
		return resBody, fmt.Errorf("response failed with status code %d", statusCode)
	}
	if err != nil {
		return nil, err
	}
	return resBody, nil
}

// BasicAuthTransport is a custom transport that adds Basic Authentication to the HTTP request
type BasicAuthTransport struct {
	Username string
	Password string
	Wrapped  http.RoundTripper
}

func (bat *BasicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(bat.Username, bat.Password)
	return bat.Wrapped.RoundTrip(req)
}

// NewGardenerManagedPrometheusClient returns an HTTP client for talking to
// the Gardener Managed Prometheus.
func NewGardenerManagedPrometheusClient(host string, username string, password string) (Client, error) {
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	// Set Basic Authentication
	client.Transport = &BasicAuthTransport{
		Username: username,
		Password: password,
		Wrapped:  http.DefaultTransport,
	}
	return &gardenerManagedPrometheusClient{
		client: client,
		uri:    "https://" + host + "/api/v1/query",
	}, nil
}

var _ Client = &gardenerManagedPrometheusClient{}
