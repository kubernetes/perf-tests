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
)

func init() {
	prometheus.MustRegister(DNSPropagationSeconds, DNSPropagationCount)
}
