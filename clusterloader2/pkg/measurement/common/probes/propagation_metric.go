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

package probes

import (
	"fmt"
	"sort"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultDNSPropagationProbeNamespaceIndex = 1
	defaultDNSPropagationProbeSampleCount    = 10
)

// NewPropagationMetricPrometheus tries to parse latency data from results of Prometheus query from propagation probe.
func NewPropagationMetricPrometheus(samples []*model.Sample) (*measurementutil.LatencyMetric, error) {
	var latencyMetric measurementutil.LatencyMetric
	values := []float64{}
	for _, sample := range samples {
		values = append(values, float64(sample.Value))
	}
	sort.Float64s(values)
	p50, err := stats.Percentile(values, 50)
	if err != nil {
		return nil, err
	}
	p90, err := stats.Percentile(values, 90)
	if err != nil {
		return nil, err
	}
	p99, err := stats.Percentile(values, 99)
	if err != nil {
		return nil, err
	}
	latencyMetric.SetQuantile(0.5, time.Duration(p50*float64(time.Second)))
	latencyMetric.SetQuantile(0.90, time.Duration(p90*float64(time.Second)))
	latencyMetric.SetQuantile(0.99, time.Duration(p99*float64(time.Second)))
	return &latencyMetric, nil
}

func InitializeTemplateMappingForDNSPropagationProbe(config *measurement.Config) (map[string]interface{}, error) {
	DNSPropagationProbeService, err := util.GetStringOrDefault(config.Params, "DNSPropagationProbeService", "")
	if err != nil {
		return nil, err
	}
	DNSPropagationProbeStatefulSet, err := util.GetStringOrDefault(config.Params, "DNSPropagationProbeStatefulSet", "")
	if err != nil {
		return nil, err
	}
	DNSPropagationProbePodCount, err := util.GetIntOrDefault(config.Params, "DNSPropagationProbePodCount", 0)
	if err != nil {
		return nil, err
	}
	DNSPropagationProbeSampleCount, err := util.GetIntOrDefault(config.Params, "DNSPropagationProbeSampleCount", defaultDNSPropagationProbeSampleCount)
	if err != nil {
		return nil, err
	}
	// Reconstructing namespace name
	DNSPropagationProbeNamespaceIndex, err := util.GetIntOrDefault(config.Params, "DNSPropagationProbeNamespaceIndex", defaultDNSPropagationProbeNamespaceIndex)
	if err != nil {
		return nil, err
	}
	namespacePrefix := config.ClusterFramework.GetAutomanagedNamespacePrefix()
	DNSPropagationProbeNamespace := fmt.Sprintf("%s-%d", namespacePrefix, DNSPropagationProbeNamespaceIndex)
	klog.V(2).Infof("DNS propagation namespace, GetAutomanagedNamespacePrefix= %s, DNSPropagationProbeNamespaceIndex=%d, DNSPropagationProbeNamespace=%s",
		namespacePrefix, DNSPropagationProbeNamespaceIndex, DNSPropagationProbeNamespace)
	return map[string]interface{}{
		"DNSPropagationProbeNamespace":   DNSPropagationProbeNamespace,
		"DNSPropagationProbeService":     DNSPropagationProbeService,
		"DNSPropagationProbeStatefulSet": DNSPropagationProbeStatefulSet,
		"DNSPropagationProbePodCount":    DNSPropagationProbePodCount,
		"DNSPropagationProbeSampleCount": DNSPropagationProbeSampleCount,
	}, nil
}
