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

package dns

import (
	"flag"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog"
	"k8s.io/perf-tests/probes/pkg/common"
)

var (
	// inClusterDNSLatency implements the In-Cluster DNS latency SLI, see
	// https://github.com/kubernetes/community/blob/master/sig-scalability/slos/dns_latency.md
	inClusterDNSLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_dns_latency_seconds",
		Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 26), // from 1us up to ~1min
		Help:      "Histogram of the time (in seconds) it took to ping a ping-server instance.",
	})
	// inClusterDNSLookupCount counts DNS lookups done by prober.
	inClusterDNSLookupCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_dns_lookup_count",
		Help:      "Counter of DNS lookups made by dns-prober.",
	})
	// inClusterDNSLookupError counts failed DNS lookups done by prober.
	inClusterDNSLookupError = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "in_cluster_dns_lookup_error",
		Help:      "Counter of DNS lookups made by dns-prober that failed",
	})

	url      = flag.String("dns-probe-url", "", "Name of a Service to lookup")
	interval = flag.Duration("dns-probe-interval", 1*time.Second, "Interval between DNS lookups")
)

func init() {
	prometheus.MustRegister(inClusterDNSLatency, inClusterDNSLookupCount, inClusterDNSLookupError)
}

// Run periodically does DNS lookups.
// Lookup's interval and URL are configurable via flags.
func Run() {
	if *url == "" {
		klog.Fatal("--dns-probe-url has not been set")
	}
	run(*url, *interval)
}

func run(url string, interval time.Duration) {
	klog.Infof("Starting dns-probe...")
	for {
		time.Sleep(interval)
		klog.V(4).Infof("dns lookup %s", url)
		startTime := time.Now()
		inClusterDNSLookupCount.Inc()
		if err := lookup(url); err != nil {
			klog.Warningf("got error: %v", err)
			inClusterDNSLookupError.Inc()
			continue
		}
		latency := time.Since(startTime)
		klog.V(4).Infof("dns lookup took %v", latency)
		inClusterDNSLatency.Observe(latency.Seconds())
	}
}

func lookup(url string) error {
	_, err := net.LookupIP(url)
	if err != nil {
		return err
	}
	return nil
}
