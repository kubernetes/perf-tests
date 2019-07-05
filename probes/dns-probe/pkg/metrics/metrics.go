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
)

const (
	namespace = "probes"
)

var (
	// InClusterDNSLatency implements the In-Cluster DNS latency SLI, see
	// https://github.com/kubernetes/community/blob/master/sig-scalability/slos/dns_latency.md
	InClusterDNSLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "in_cluster_dns_latency_seconds",
		Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 26), // from 1us up to ~1min
		Help:      "Histogram of the time (in seconds) it took to ping a ping-server instance.",
	})
	// InClusterDNSLookupCount counts DNS lookups done by prober.
	InClusterDNSLookupCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "in_cluster_dns_lookup_count",
		Help:      "Counter of DNS lookups made by dns-prober.",
	})
	// InClusterDNSLookupError counts failed DNS lookups done by prober.
	InClusterDNSLookupError = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "in_cluster_dns_lookup_error",
		Help:      "Counter of DNS lookups made by dns-prober that failed",
	})
)

func init() {
	prometheus.MustRegister(InClusterDNSLatency, InClusterDNSLookupCount, InClusterDNSLookupError)
}
