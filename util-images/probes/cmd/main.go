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

package main

import (
	"flag"
	"net/http"
	_ "net/http/pprof"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/probes/pkg/dns"
	pingclient "k8s.io/perf-tests/util-images/probes/pkg/ping/client"
	pingserver "k8s.io/perf-tests/util-images/probes/pkg/ping/server"
)

var (
	metricAddress = flag.String("metric-bind-address", "0.0.0.0:8080", "The address to serve the Prometheus metrics on.")
	mode          = flag.String("mode", "", "Mode that should be run. Supported values: ping-server, ping-client, dns")
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	klog.Infof("I'm probes.")
	klog.Infof("Mode is: %s\n", *mode)

	exposeMetrics()
	// TODO(mm4tt): Implement readiness probes.

	switch *mode {
	case "ping-client":
		pingclient.Run(pingclient.NewDefaultPingClientConfig())
	case "ping-server":
		pingserver.Run(pingserver.NewDefaultPingServerConfig())
	case "dns":
		dns.Run()
	default:
		klog.Fatalf("Unrecognized mode: %q", *mode)
	}
}

func exposeMetrics() {
	if *metricAddress == "" {
		klog.Fatal("--metric-bind-address not set!")
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		klog.Infof("Serving metrics on %s\n", *metricAddress)
		klog.Fatal(http.ListenAndServe(*metricAddress, nil))
	}()
}
