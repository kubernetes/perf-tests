/*
Copyright 2024 The Kubernetes Authors.

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

package dnspropagation

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/perf-tests/util-images/probes/pkg/common"
)

var (
	// DNSPropagationSeconds denotes the propagation time of a given StatefulSet, calculated as the average difference between the pod's "running" and "discoverable" timestamps.
	DNSPropagationSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_seconds",
		Help:      "Gauge of the time (in seconds) it took for the pods in a statefulSet to be discoverable after they start running.",
	}, []string{"namespace", "service", "podName"})
	// DNSPropagationCount denotes the number of DNS propagation checks performed.
	DNSPropagationCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_count",
		Help:      "Counter of the number of DNS propagation checks performed.",
	}, []string{"namespace", "service", "podName"})
	// DNSLookupErrors denotes the total number of failed DNS lookups.
	DNSLookupErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_lookup_errors_total",
		Help:      "Counter of the total number of DNS lookup errors.",
	}, []string{"namespace", "service", "podName"})

	// The stage gauges split dns_propagation_seconds of each sampled pod into the parts below.
	// Ready times have second resolution, so stages that start at Ready include up to 1s of rounding.

	// DNSPropagationPodSeenSeconds is the time from the pod's Ready transition until the prober saw the pod Ready.
	DNSPropagationPodSeenSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_pod_seen_seconds",
		Help:      "Gauge of the time (in seconds) from the pod's Ready transition until the prober saw the pod Ready.",
	}, []string{"namespace", "service", "podName"})
	// DNSPropagationEndpointSeconds is the time from the pod's Ready transition until the prober saw the pod as a
	// ready endpoint of the service.
	DNSPropagationEndpointSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_endpoint_seconds",
		Help:      "Gauge of the time (in seconds) from the pod's Ready transition until the prober saw the pod as a ready endpoint of the service.",
	}, []string{"namespace", "service", "podName"})
	// DNSPropagationAfterEndpointSeconds is the time from the prober seeing the endpoint until the first successful
	// lookup. It is negative when DNS resolved before the prober saw the endpoint.
	DNSPropagationAfterEndpointSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_after_endpoint_seconds",
		Help:      "Gauge of the time (in seconds) from the prober seeing the pod as a ready endpoint until the first successful lookup.",
	}, []string{"namespace", "service", "podName"})
	// DNSPropagationLookupSeconds is the time from the prober seeing the pod Ready until the first successful lookup.
	DNSPropagationLookupSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: common.ProbeNamespace,
		Name:      "dns_propagation_lookup_seconds",
		Help:      "Gauge of the time (in seconds) from the prober seeing the pod Ready until the first successful lookup.",
	}, []string{"namespace", "service", "podName"})
)

func init() {
	prometheus.MustRegister(DNSPropagationSeconds, DNSPropagationCount, DNSLookupErrors,
		DNSPropagationPodSeenSeconds, DNSPropagationEndpointSeconds, DNSPropagationAfterEndpointSeconds,
		DNSPropagationLookupSeconds)
}
