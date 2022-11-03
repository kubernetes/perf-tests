/*
Copyright 2021 The Kubernetes Authors.

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

package common

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	nodelocaldnsLatencyPrometheusMeasurementName = "NodeLocalDNSLatencyPrometheus"
	percLatencyQueryTemplate                     = `histogram_quantile(%v, sum(rate(coredns_dns_request_duration_seconds_bucket[%v])) by (le))`
	defaultThreshold                             = 5 * time.Second
)

var (
	desiredPercentiles = []float64{0.5, 0.9, 0.99}
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&nodelocaldnsLatencyGatherer{})
	}
	if err := measurement.Register(nodelocaldnsLatencyPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", nodelocaldnsLatencyPrometheusMeasurementName, err)
	}
}

type nodelocaldnsLatencyGatherer struct{}

func (n *nodelocaldnsLatencyGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	result, err := n.getPercentileLatencies(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(result.ToPerfData(n.String()))
	if err != nil {
		return nil, err
	}
	summaries := []measurement.Summary{measurement.CreateSummary(n.String(), "json", content)}
	return summaries, n.validateResult(config, result)
}

func (n *nodelocaldnsLatencyGatherer) String() string {
	return nodelocaldnsLatencyPrometheusMeasurementName
}

func (n *nodelocaldnsLatencyGatherer) Configure(config *measurement.Config) error {
	return nil
}
func (n *nodelocaldnsLatencyGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (n *nodelocaldnsLatencyGatherer) validateResult(config *measurement.Config, result *measurementutil.LatencyMetric) error {
	latencyUpperBound, err := util.GetDurationOrDefault(config.Params, "threshold", defaultThreshold)
	if err != nil {
		return err
	}
	if result.Perc99 > latencyUpperBound {
		return errors.NewMetricViolationError(
			"NodelocalDNS dns_request_duration_seconds",
			fmt.Sprintf("99th Percentile Latency %v is higher than the upper bound of %s", result.Perc99, latencyUpperBound))
	}
	return nil
}

func (n *nodelocaldnsLatencyGatherer) getPercentileLatencies(executor QueryExecutor, startTime, endTime time.Time) (*measurementutil.LatencyMetric, error) {
	measurementDuration := endTime.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)
	errList := errors.NewErrorList()
	result := &measurementutil.LatencyMetric{}
	for _, percVal := range desiredPercentiles {
		query := fmt.Sprintf(percLatencyQueryTemplate, percVal, promDuration)
		samples, err := executor.Query(query, endTime)
		if err != nil {
			errList.Append(fmt.Errorf("failed to execute query %q, err - %v", query, err))
			continue
		}
		if len(samples) != 1 {
			errList.Append(fmt.Errorf("got unexpected number of samples: %d for query %q", len(samples), query))
			continue
		}
		result.SetQuantile(percVal, time.Duration(float64(samples[0].Value)*float64(time.Second)))
	}
	if !errList.IsEmpty() {
		return nil, fmt.Errorf("failed to compute latencies, errors - %s", errList.Error())
	}
	return result, nil
}
