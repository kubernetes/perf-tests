/*
Copyright 2023 The Kubernetes Authors.

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
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/common/model"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	watchListLatencyPrometheusMeasurementName = "WatchListLatencyPrometheus"

	// watchListLatencyQuery placeholders must be replaced with (1) quantile (2) query window size
	watchListLatencyQuery = "histogram_quantile(%.2f, sum(rate(apiserver_watch_list_duration_seconds{}[%v])) by (group, version, resource, scope, le))"
)

func init() {
	create := func() measurement.Measurement { return CreatePrometheusMeasurement(&watchListLatencyGatherer{}) }
	if err := measurement.Register(watchListLatencyPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", watchListLatencyPrometheusMeasurementName, err)
	}
}

type watchListLatencyMetric struct {
	Group    string                        `json:"group"`
	Version  string                        `json:"version"`
	Resource string                        `json:"resource"`
	Scope    string                        `json:"scope"`
	Latency  measurementutil.LatencyMetric `json:"latency"`
}

type watchListLatencyMetrics map[string]*watchListLatencyMetric

func (m watchListLatencyMetrics) SetLatency(group, version, resource, scope string, quantile float64, latency time.Duration) {
	key := fmt.Sprintf("%s|%s|%s", group, resource, scope)
	entry, exists := m[key]
	if !exists {
		entry = &watchListLatencyMetric{
			Group:    group,
			Version:  version,
			Resource: resource,
			Scope:    scope,
		}
		m[key] = entry
	}
	entry.Latency.SetQuantile(quantile, latency)
}

// watchListLatencyGatherer gathers 50th, 90th and 99th duration quantiles
// for watch list requests broken down by group, resource, scope.
type watchListLatencyGatherer struct{}

func (m *watchListLatencyGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	rawWatchListMetrics, err := gatherWatchListLatencyPrometheusSamples(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}
	watchListMetrics, err := convertWatchListPrometheusSamplesToWatchListLatencyMetrics(rawWatchListMetrics)
	if err != nil {
		return nil, err
	}
	watchListMetricsJSON, err := util.PrettyPrintJSON(convertWatchListLatencyMetricsToPerfData(watchListMetrics))
	if err != nil {
		return nil, err
	}
	summaryName, err := util.GetStringOrDefault(config.Params, "summaryName", m.String())
	if err != nil {
		return nil, err
	}
	summaries := []measurement.Summary{
		measurement.CreateSummary(summaryName, "json", watchListMetricsJSON),
	}
	return summaries, nil
}

func (m *watchListLatencyGatherer) Configure(_ *measurement.Config) error { return nil }
func (m *watchListLatencyGatherer) IsEnabled(_ *measurement.Config) bool  { return true }
func (m *watchListLatencyGatherer) String() string                        { return watchListLatencyPrometheusMeasurementName }

func gatherWatchListLatencyPrometheusSamples(executor QueryExecutor, startTime, endTime time.Time) ([]*model.Sample, error) {
	var latencySamples []*model.Sample
	// since we collect LatencyMetric only 0.5, 0.9 and 0.99 quantiles are supported
	quantiles := []float64{0.5, 0.9, 0.99}
	measurementDuration := endTime.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)

	for _, q := range quantiles {
		query := fmt.Sprintf(watchListLatencyQuery, q, promDuration)
		samples, err := executor.Query(query, endTime)
		if err != nil {
			return nil, err
		}
		for _, sample := range samples {
			sample.Metric["quantile"] = model.LabelValue(fmt.Sprintf("%.2f", q))
		}
		latencySamples = append(latencySamples, samples...)
	}

	return latencySamples, nil
}

func convertWatchListPrometheusSamplesToWatchListLatencyMetrics(latencySamples []*model.Sample) (watchListLatencyMetrics, error) {
	latencyMetrics := make(watchListLatencyMetrics)
	extractLabels := func(sample *model.Sample) (string, string, string, string) {
		return string(sample.Metric["group"]), string(sample.Metric["version"]), string(sample.Metric["resource"]), string(sample.Metric["scope"])
	}

	for _, sample := range latencySamples {
		group, version, resource, scope := extractLabels(sample)
		quantile, err := strconv.ParseFloat(string(sample.Metric["quantile"]), 64)
		if err != nil {
			return nil, err
		}

		latency := time.Duration(float64(sample.Value) * float64(time.Second))
		latencyMetrics.SetLatency(group, version, resource, scope, quantile, latency)
	}

	return latencyMetrics, nil
}

func convertWatchListLatencyMetricsToPerfData(watchListMetrics watchListLatencyMetrics) *measurementutil.PerfData {
	var watchListMetricsSlice []*watchListLatencyMetric
	for _, v := range watchListMetrics {
		watchListMetricsSlice = append(watchListMetricsSlice, v)
	}
	sort.Slice(watchListMetricsSlice, func(i, j int) bool {
		return watchListMetricsSlice[i].Latency.Perc99 > watchListMetricsSlice[j].Latency.Perc99
	})

	perfData := &measurementutil.PerfData{Version: "v1"}
	for _, watchListMetric := range watchListMetricsSlice {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Perc50": float64(watchListMetric.Latency.Perc50) / 1000000,
				"Perc90": float64(watchListMetric.Latency.Perc90) / 1000000,
				"Perc99": float64(watchListMetric.Latency.Perc99) / 1000000,
			},
			Unit: "ms",
			Labels: map[string]string{
				"Group":    watchListMetric.Group,
				"Version":  watchListMetric.Version,
				"Resource": watchListMetric.Resource,
				"Scope":    watchListMetric.Scope,
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}
