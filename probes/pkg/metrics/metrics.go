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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/perf-tests/probes/pkg/common"
)

var (
	// InClusterNetworkLatency implements the In-Cluster Network Programming SLI, see
	// https://github.com/kubernetes/community/blob/master/sig-scalability/slos/network_latency.md
	InClusterNetworkLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_network_latency_seconds",
		Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 26), // from 1us up to ~1min
		Help:      "Histogram of the time (in seconds) it took to ping a ping-server instance.",
	})
)

func init() {
	prometheus.MustRegister(InClusterNetworkLatency)
}
