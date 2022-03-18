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

package slos

import (
	"fmt"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	netProg = "NetworkProgrammingLatency"

	metricVersion = "v1"

	// Query measuring 99th percentile of Xth percentiles (where X=50,90,99) of network programming latency over last 5min.
	// %v should be replaced with query window size (duration of the test).
	// This measurement assumes, that there is no data points for the rest of the cluster-day.
	// Definition: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/network_programming_latency.md
	query = "quantile_over_time(0.99, kubeproxy:kubeproxy_network_programming_duration:histogram_quantile{}[%v])"
)

func init() {
	create := func() measurement.Measurement { return common.CreatePrometheusMeasurement(&netProgGatherer{}) }
	if err := measurement.Register(netProg, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", netProg, err)
	}
}

type netProgGatherer struct{}

func (n *netProgGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (n *netProgGatherer) IsEnabled(config *measurement.Config) bool {
	// Disable NetworkProgrammingLatency measurement if scraping kube-proxy is disabled.
	if !config.ClusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy {
		return false
	}
	// TODO(#1399): remove the dependency of provider name.
	return config.CloudProvider.Name() != "kubemark"
}

func (n *netProgGatherer) Gather(executor common.QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	latency, err := n.query(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}

	klog.V(2).Infof("%s: got %v", netProg, latency)
	summary, err := n.createSummary(latency)
	return []measurement.Summary{summary}, err
}

func (n *netProgGatherer) String() string {
	return netProg
}

func (n *netProgGatherer) query(executor common.QueryExecutor, startTime, endTime time.Time) (*measurementutil.LatencyMetric, error) {
	duration := endTime.Sub(startTime)

	boundedQuery := fmt.Sprintf(query, measurementutil.ToPrometheusTime(duration))

	samples, err := executor.Query(boundedQuery, endTime)
	if err != nil {
		return nil, err
	}
	if len(samples) != 3 {
		return nil, fmt.Errorf("got unexpected number of samples: %d", len(samples))
	}
	return measurementutil.NewLatencyMetricPrometheus(samples)
}

func (n *netProgGatherer) createSummary(latency *measurementutil.LatencyMetric) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version:   metricVersion,
		DataItems: []measurementutil.DataItem{latency.ToPerfData(netProg)},
	})
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(netProg, "json", content), nil
}
