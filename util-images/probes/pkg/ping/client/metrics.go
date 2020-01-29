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

package pingclient

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/perf-tests/util-images/probes/pkg/common"
)

var (
	// inClusterNetworkLatency implements the In-Cluster Network Programming SLI, see
	// https://github.com/kubernetes/community/blob/master/sig-scalability/slos/network_latency.md
	inClusterNetworkLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_network_latency_seconds",
		Buckets: merge(
			prometheus.LinearBuckets(0.025, 0.025, 3), // 25ms, 50ms, 75ms
			prometheus.LinearBuckets(0.1, 0.05, 18),   // 100ms, 150ms, 200ms... 950ms
			prometheus.LinearBuckets(1, 1, 5),         // 1s, 2s, 3s, 4s, 5s
			prometheus.LinearBuckets(10, 5, 5),        // 10s, 15s, 20s, 25s, 30s
		),
		Help: "Histogram of the time (in seconds) it took to ping a ping-server instance.",
	})
	// inClusterDNSLookupCount counts DNS lookups done by prober.
	inClusterNetworkLatencyPingCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_network_latency_ping_count",
		Help:      "Counter of pings by ping-client.",
	})
	// inClusterDNSLookupError counts failed DNS lookups done by prober.
	inClusterNetworkLatencyError = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_network_latency_error",
		Help:      "Counter of pings by ping-client  that failed",
	})
)

func init() {
	prometheus.MustRegister(inClusterNetworkLatency, inClusterNetworkLatencyPingCount, inClusterNetworkLatencyError)
}
