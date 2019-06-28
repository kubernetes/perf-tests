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
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	netProg = "NetworkProgrammingLatency"

	metricVersion = "v1"

	// Query measuring 99th percentaile of network programming latency.
	// %v should be replaced with query window size (duration of the test).
	// This measurement assumes, that there is no data points for the rest of the cluster-day.
	// Definition: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/network_programming_latency.md
	query = "quantile_over_time(0.99, kubeproxy:kubeproxy_network_programming_duration:histogram_quantile{quantile='0.99'}[%v])"
)

func init() {
	create := func() measurement.Measurement { return createPrometheusMeasurement(&netProgGatherer{}) }
	if err := measurement.Register(netProg, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", netProg, err)
	}
}

type netProgGatherer struct{}

func (n *netProgGatherer) Gather(executor QueryExecutor, startTime time.Time) (measurement.Summary, error) {
	latency, err := n.query(executor, startTime)
	if err != nil {
		return nil, err
	}
	klog.Infof("%s: 99th percentaile of network programming latency %.2f ms", netProg, latency)
	return n.createSummary(latency)
}

func (n *netProgGatherer) String() string {
	return netProg
}

func (n *netProgGatherer) query(executor QueryExecutor, startTime time.Time) (float64, error) {
	end := time.Now()
	duration := end.Sub(startTime)

	boundedQuery := fmt.Sprintf(query, measurementutil.ToPrometheusTime(duration))

	samples, err := executor.Query(boundedQuery, end)
	if err != nil {
		return 0, err
	}
	if len(samples) != 1 {
		return 0, fmt.Errorf("got unexpected number of samples: %d", len(samples))
	}
	return float64(samples[0].Value) * 1000, nil // s -> ms
}

func (n *netProgGatherer) createSummary(latency float64) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(n.createPerfData(latency))
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(netProg, "json", content)
	return summary, nil
}

func (n *netProgGatherer) createPerfData(latency float64) *measurementutil.PerfData {
	return &measurementutil.PerfData{
		Version: metricVersion,
		DataItems: []measurementutil.DataItem{{
			Data: map[string]float64{"NetProgSLI": latency},
			Unit: "ms",
		}},
	}
}
