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

package common

import (
	"fmt"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	metricsServerPrometheusMeasurementName = "MetricsServerPrometheus"

	metricsServerLatencyQuery = `histogram_quantile(%v, sum(rate(apiserver_request_duration_seconds_bucket{group="metrics.k8s.io",resource="pods",scope="cluster"}[%v])) by (le))`
)

var (
	desiredMsPercentiles = []float64{0.5, 0.9, 0.99}
)

func init() {
	create := func() measurement.Measurement { return CreatePrometheusMeasurement(&metricsServerGatherer{}) }
	if err := measurement.Register(metricsServerPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", metricsServerPrometheusMeasurementName, err)
	}
}

type metricsServerGatherer struct{}

func (g *metricsServerGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	latencyMetrics, err := g.gatherLatencyMetrics(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(latencyMetrics)
	if err != nil {
		return nil, err
	}
	summaries := []measurement.Summary{measurement.CreateSummary(metricsServerPrometheusMeasurementName, "json", content)}
	return summaries, nil
}

func (g *metricsServerGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (g *metricsServerGatherer) IsEnabled(config *measurement.Config) bool {
	return config.CloudProvider.Features().SupportMetricsServerMetrics
}

func (g *metricsServerGatherer) String() string {
	return metricsServerPrometheusMeasurementName
}

func (g *metricsServerGatherer) gatherLatencyMetrics(executor QueryExecutor, startTime, endTime time.Time) (*measurementutil.LatencyMetric, error) {
	measurementDuration := endTime.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)

	errList := errors.NewErrorList()
	result := &measurementutil.LatencyMetric{}

	for _, percentile := range desiredMsPercentiles {

		query := fmt.Sprintf(metricsServerLatencyQuery, percentile, promDuration)
		samples, err := executor.Query(query, endTime)
		if err != nil {
			errList.Append(fmt.Errorf("failed to execute query %q, err - %v", query, err))
			continue
		}

		if len(samples) != 1 {
			errList.Append(fmt.Errorf("got unexpected number of samples: %d for query %q", len(samples), query))
			continue
		}

		result.SetQuantile(percentile, time.Duration(float64(samples[0].Value)*float64(time.Second)))
	}

	if !errList.IsEmpty() {
		return nil, fmt.Errorf("failed to compute latencies, errors - %s", errList.Error())
	}

	return result, nil
}
