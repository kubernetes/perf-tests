/*
Copyright 2019 The Kubernetes Authors.

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

package kubeclient

import (
	"context"
	"flag"
	"fmt"
	"time"

	"k8s.io/klog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	groupedResourceURITemplate    = "/apis/%s/%s/%s"
	nonGroupedResourceURITemplate = "/api/%s/%s"
)

var (
	resourceGroup   = flag.String("kubeclient-probe-resourceGroup", "", "Target resource group to be get/list")
	resourceVersion = flag.String("kubeclient-probe-resourceVersion", "", "Target resource version to be get/list")
	resourceName    = flag.String("kubeclient-probe-resourceName", "", "Target resourceName to be get/list")
	interval        = flag.Duration("kubeclient-probe-interval", 30*time.Second, "Probe interval, defaults 30s")
)

type Config struct {
	group    string
	version  string
	name     string
	interval time.Duration
}

func Run(config *Config) {
	// creates the in-cluster config
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Can not build inClusterConfig, error:%v", err)
	}
	// creates the inCluster kube client
	clientset, err := kubernetes.NewForConfig(kubeConfig)

	if err != nil {
		klog.Fatalf("Failed to build kubeClient, error:%v", err)
	}

	runProbe(clientset, config)
}

func NewDefaultConfig() *Config {
	if *resourceVersion == "" {
		klog.Fatal("--kubeclient-probe-resourceVersion must be provided")
	}

	if *resourceName == "" {
		klog.Fatal("--kubeclient-probe-resourceName must be provided")
	}

	return &Config{
		group:    *resourceGroup,
		version:  *resourceVersion,
		name:     *resourceName,
		interval: *interval,
	}
}

func runProbe(client kubernetes.Interface, config *Config) {
	var requestURL = ""
	if config.group != "" {
		requestURL = fmt.Sprintf(groupedResourceURITemplate, config.group, config.version, config.name)
	} else {
		requestURL = fmt.Sprintf(nonGroupedResourceURITemplate, config.version, config.name)
	}

	tick := time.NewTicker(config.interval)
	for {
		select {
		case <-tick.C:
			out, err := doProbe(client, requestURL, config.interval)
			if err != nil {
				klog.Errorf("Probe apiserver failed, error:%v", err)
				continue
			}

			klog.V(2).Infof("Probe apiserver successfully, output:%s", string(out))
		}
	}
}

func doProbe(client kubernetes.Interface, requestURL string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return client.CoreV1().RESTClient().Get().RequestURI(requestURL).Do(ctx).Raw()
}
