/*
Copyright 2017 The Kubernetes Authors.

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
	"flag"
	"net/http"
	"time"

	"k8s.io/perf-tests/slo-monitor/src/monitors"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
)

var (
	apiServerAddress  string
	kubeconfigFile    string
	kubernetesURL     string
	listenURL         string
	purgeAfterSeconds int32
)

func registerFlags(fs *pflag.FlagSet) {
	fs.StringVar(&apiServerAddress, "apiserver-address", "",
		`The address of the Kubernetes Apiserver to connect to in the format of
protocol://address:port, e.g., http://localhost:8080. If not specified, the
assumption is that the binary runs inside a Kubernetes cluster and local
discovery is attempted.`)
	fs.StringVar(&kubeconfigFile, "kubeconfig", "",
		`Path to kubeconfig file with authorization and master location information.`)
	fs.StringVar(&listenURL, "listen-url", ":8080",
		`URL on which monitor should serve metrics.`)
	fs.Int32Var(&purgeAfterSeconds, "purge-after-seconds", 120,
		`Time after which deleted entries are purged.`)

	// DEPRECATED
	fs.StringVar(&kubernetesURL, "kubernetes-url", "",
		`Kubernetes master location. Leave blank for default.`)
}

func createKubeClient() (clientset.Interface, error) {
	kubeConfig, err := clientcmd.BuildConfigFromFlags(apiServerAddress, kubeconfigFile)
	if err != nil {
		return nil, err
	}
	kubeConfig.ContentType = "application/vnd.kubernetes.protobuf"

	return clientset.NewForConfig(kubeConfig)
}

func main() {
	registerFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	glog.Infof("Starting Performance SLO monitor on port %v", listenURL)

	monitors.Register()
	http.Handle("/metrics", prometheus.Handler())

	kubeClient, err := createKubeClient()
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	monitor := monitors.NewPodStartupLatencyDataMonitor(kubeClient, time.Duration(purgeAfterSeconds)*time.Second)
	go func() {
		if err := monitor.Run(stopCh); err != nil {
			panic(err)
		}
	}()

	glog.Fatal(http.ListenAndServe(listenURL, nil))
}
