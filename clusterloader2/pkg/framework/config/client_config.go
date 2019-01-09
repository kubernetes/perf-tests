/*
Copyright 2018 The Kubernetes Authors.

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

package config

import (
	"fmt"
	"net"
	"net/http"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/transport"
)

const (
	contentType = "application/vnd.kubernetes.protobuf"
	qps         = 100
	burst       = 200
)

// PrepareConfig creates and initializes client config.
func PrepareConfig(path string) (*restclient.Config, error) {
	config, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	if err = initializeWithDefaults(config); err != nil {
		return nil, fmt.Errorf("config initialization error: %v", err)
	}
	return config, nil
}

func restclientConfig(path string) (*clientcmdapi.Config, error) {
	c, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("error loading kubeconfig: %v", err)
	}
	return c, nil
}

func loadConfig(path string) (*restclient.Config, error) {
	c, err := restclientConfig(path)
	if err != nil {
		return nil, err
	}
	return clientcmd.NewDefaultClientConfig(*c, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func initializeWithDefaults(config *restclient.Config) error {
	config.ContentType = contentType
	config.QPS = qps
	config.Burst = burst

	// For the purpose of this test, we want to force that clients
	// do not share underlying transport (which is a default behavior
	// in Kubernetes). Thus, we are explicitly creating transport for
	// each client here.
	transportConfig, err := config.TransportConfig()
	if err != nil {
		return err
	}
	tlsConfig, err := transport.TLSConfigFor(transportConfig)
	if err != nil {
		return err
	}
	config.Transport = utilnet.SetTransportDefaults(&http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     tlsConfig,
		MaxIdleConnsPerHost: 100,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	})
	// Overwrite TLS-related fields from config to avoid collision with
	// Transport field.
	config.TLSClientConfig = restclient.TLSClientConfig{}

	return nil
}
