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

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client kubernetes.Clientset

type Response struct {
	Duration time.Duration
	Raw      []byte
	Error    error
}

func (c *Client) TimedRequest(ctx context.Context, request *rest.Request) Response {
	start := time.Now()
	response, err := request.MaxRetries(0).DoRaw(ctx)
	duration := time.Since(start)

	return Response{
		Duration: duration,
		Raw:      response,
		Error:    err,
	}
}

func GetClient() (*Client, error) {
	config, err := getConfig()
	if err != nil {
		return nil, err
	}

	config.QPS = -1 // Disable rate limiter

	client, err := kubernetes.NewForConfig(config)
	return (*Client)(client), err
}

func getConfig() (*rest.Config, error) {
	if _, ok := os.LookupEnv("KUBERNETES_PORT"); ok {
		return rest.InClusterConfig()
	}

	if kubeconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if home, ok := os.LookupEnv("HOME"); ok {
		kubeconfig := filepath.Join(home, ".kube", "config")
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	return nil, fmt.Errorf("could not create client-go config")
}
