/*
Copyright 2025 The Kubernetes Authors.

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
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	// prometheusSchedulingMetricsMeasurementName is the identifier used in ClusterLoader2 config files.
	// It intentionally differs from schedulerLatencyMetricName to let users choose which implementation
	// they want to run (direct scrape vs Prometheus based) without changing the output payload.
	prometheusSchedulingMetricsMeasurementName = "PrometheusSchedulingMetrics"
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&prometheusSchedulerLatencyGatherer{})
	}
	if err := measurement.Register(prometheusSchedulingMetricsMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", prometheusSchedulingMetricsMeasurementName, err)
	}
}

// prometheusSchedulerLatencyGatherer implements the Gatherer interface and generates the same
// JSON summary as scheduler_latency.go but sources the histogram data from Prometheus instead of
// scraping kube-scheduler directly.
//
// The Gather method uses Prometheus' increase() function to calculate bucket increments over the
// test window [startTime, endTime). Those increments are then converted into Histogram structures
// identical to the ones produced by the in-cluster scrape implementation, after which existing
// helper code is re-used to compute the 50th/90th/99th percentiles.
//
// No parameters are required – the measurement automatically queries all buckets for the four
// relevant histograms.
//
// NOTE: This measurement assumes that the kube-scheduler metrics endpoint is being scraped by
// Prometheus and that the metric names match the upstream defaults.

type prometheusSchedulerLatencyGatherer struct{}

func (g *prometheusSchedulerLatencyGatherer) Configure(_ *measurement.Config) error { return nil }
func (g *prometheusSchedulerLatencyGatherer) IsEnabled(_ *measurement.Config) bool  { return true }
func (g *prometheusSchedulerLatencyGatherer) String() string {
	return prometheusSchedulingMetricsMeasurementName
}

func (g *prometheusSchedulerLatencyGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	window := endTime.Sub(startTime)
	promWindow := measurementutil.ToPrometheusTime(window)

	queryHistogram := func(metric string, labelFilter string) (*measurementutil.Histogram, error) {
		var query string
		if labelFilter != "" {
			query = fmt.Sprintf(`sum(increase(%s{%s}[%s])) by (le)`, metric, labelFilter, promWindow)
		} else {
			query = fmt.Sprintf(`sum(increase(%s[%s])) by (le)`, metric, promWindow)
		}

		samples, err := executor.Query(query, endTime)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query %q: %w", query, err)
		}

		hist := measurementutil.NewHistogram(nil)
		for _, s := range samples {
			measurementutil.ConvertSampleToHistogram(s, hist)
		}
		return hist, nil
	}

	metrics := schedulerLatencyMetrics{
		e2eSchedulingDurationHist:           measurementutil.NewHistogram(nil),
		schedulingAlgorithmDurationHist:     measurementutil.NewHistogram(nil),
		preemptionEvaluationHist:            measurementutil.NewHistogram(nil),
		frameworkExtensionPointDurationHist: make(map[string]*measurementutil.Histogram),
	}

	for _, ep := range extentionsPoints {
		metrics.frameworkExtensionPointDurationHist[ep] = measurementutil.NewHistogram(nil)
	}

	var errList []error

	if h, err := queryHistogram(string(e2eSchedulingDurationMetricName), ""); err != nil {
		errList = append(errList, err)
	} else {
		metrics.e2eSchedulingDurationHist = h
	}

	if h, err := queryHistogram(string(schedulingAlgorithmDurationMetricName), ""); err != nil {
		errList = append(errList, err)
	} else {
		metrics.schedulingAlgorithmDurationHist = h
	}

	if h, err := queryHistogram(string(preemptionEvaluationMetricName), ""); err != nil {
		errList = append(errList, err)
	} else {
		metrics.preemptionEvaluationHist = h
	}

	for _, ep := range extentionsPoints {
		labelSel := fmt.Sprintf(`extension_point="%s"`, ep)
		h, err := queryHistogram(string(frameworkExtensionPointDurationMetricName), labelSel)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		metrics.frameworkExtensionPointDurationHist[ep] = h
	}

	if len(errList) > 0 {
		return nil, fmt.Errorf("prometheus scheduler latency gathering errors: %v", errList)
	}

	slm := &schedulerLatencyMeasurement{}
	result, err := slm.setQuantiles(metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to compute quantiles: %w", err)
	}

	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}

	summaries := []measurement.Summary{
		measurement.CreateSummary(config.Identifier+"_"+prometheusSchedulingMetricsMeasurementName, "json", content),
	}
	return summaries, nil
}
