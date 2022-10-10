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
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/oauth2/google"
)

// gcpManagedPrometheusClient talks to the Google Cloud Managed Service for Prometheus.
// This only works if the cluster is hosted in GCP.
// Details: https://cloud.google.com/stackdriver/docs/managed-prometheus.
type gcpManagedPrometheusClient struct {
	client *http.Client
	uri    string
}

func (mpc *gcpManagedPrometheusClient) Query(query string, queryTime time.Time) ([]byte, error) {
	params := url.Values{}
	params.Add("query", query)
	params.Add("time", queryTime.Format(time.RFC3339))
	res, err := mpc.client.Get(mpc.uri + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	resBody, err := ioutil.ReadAll(res.Body)
	if statusCode := res.StatusCode; statusCode > 299 {
		return resBody, fmt.Errorf("response failed with status code %d", statusCode)
	}
	if err != nil {
		return nil, err
	}
	return resBody, nil
}

// NewGCPManagedPrometheusClient returns an HTTP client for talking to
// the Google Cloud Managed Service for Prometheus.
func NewGCPManagedPrometheusClient() (Client, error) {
	client, err := google.DefaultClient(context.TODO(), "https://www.googleapis.com/auth/monitoring.read")
	if err != nil {
		return nil, err
	}
	return &gcpManagedPrometheusClient{
		client: client,
		uri:    fmt.Sprintf("https://monitoring.googleapis.com/v1/projects/%s/location/global/prometheus/api/v1/query", os.Getenv("PROJECT")),
	}, nil
}

var _ Client = &gcpManagedPrometheusClient{}
