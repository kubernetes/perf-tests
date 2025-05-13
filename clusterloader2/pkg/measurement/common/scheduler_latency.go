/*
Copyright 2018 The Kubernetes Authors.

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
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulerLatencyMetricName = "SchedulingMetrics"

	e2eSchedulingDurationMetricName           = model.LabelValue("scheduler_e2e_scheduling_duration_seconds_bucket")
	schedulingAlgorithmDurationMetricName     = model.LabelValue("scheduler_scheduling_algorithm_duration_seconds_bucket")
	frameworkExtensionPointDurationMetricName = model.LabelValue("scheduler_framework_extension_point_duration_seconds_bucket")
	preemptionEvaluationMetricName            = model.LabelValue("scheduler_scheduling_algorithm_preemption_evaluation_seconds_bucket")

	singleRestCallTimeout = 5 * time.Minute
	defaultMetricsPath    = "/metrics"
)

var (
	extentionsPoints = []string{
		"PreFilter",
		"Filter",
		"PostFilter",
		"PreScore",
		"Score",
		"PreBind",
		"Bind",
		"PostBind",
		"Reserve",
		"Unreserve",
		"Permit",
	}
)

func init() {
	if err := measurement.Register(schedulerLatencyMetricName, createSchedulerLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", schedulerLatencyMetricName, err)
	}
}

func createSchedulerLatencyMeasurement() measurement.Measurement {
	return &schedulerLatencyMeasurement{}
}

type schedulerLatencyMeasurement struct {
	initialLatency schedulerLatencyMetrics
}

type schedulerLatencyMetrics struct {
	e2eSchedulingDurationHist           *measurementutil.Histogram
	schedulingAlgorithmDurationHist     *measurementutil.Histogram
	preemptionEvaluationHist            *measurementutil.Histogram
	frameworkExtensionPointDurationHist map[string]*measurementutil.Histogram
}

// Execute supports two actions:
// - reset - Resets latency data on api scheduler side.
// - gather - Gathers and prints current scheduler latency data.
func (s *schedulerLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	endpoint, err := util.GetStringOrDefault(config.Params, "endpoint", "localhost:10259")
	if err != nil {
		return nil, err
	}

	token, err := util.GetStringOrDefault(config.Params, "token", "")
	if err != nil {
		return nil, err
	}

	switch action {
	case "reset":
		klog.V(2).Infof("%s: start collecting latency initial metrics in scheduler...", s)
		return nil, s.getSchedulingInitialLatency(endpoint, token)
	case "start":
		klog.V(2).Infof("%s: start collecting latency metrics in scheduler...", s)
		return nil, s.getSchedulingInitialLatency(endpoint, token)
	case "gather":
		klog.V(2).Infof("%s: gathering latency metrics in scheduler...", s)
		return s.getSchedulingLatency(endpoint, token)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (*schedulerLatencyMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*schedulerLatencyMeasurement) String() string {
	return schedulerLatencyMetricName
}

// HistogramSub is a helper function to substract two histograms
func HistogramSub(finalHist, initialHist *measurementutil.Histogram) *measurementutil.Histogram {
	for k := range finalHist.Buckets {
		finalHist.Buckets[k] = finalHist.Buckets[k] - initialHist.Buckets[k]
	}
	return finalHist
}

func (m *schedulerLatencyMetrics) substract(sub schedulerLatencyMetrics) {
	if sub.preemptionEvaluationHist != nil {
		m.preemptionEvaluationHist = HistogramSub(m.preemptionEvaluationHist, sub.preemptionEvaluationHist)
	}
	if sub.schedulingAlgorithmDurationHist != nil {
		m.schedulingAlgorithmDurationHist = HistogramSub(m.schedulingAlgorithmDurationHist, sub.schedulingAlgorithmDurationHist)
	}
	if sub.e2eSchedulingDurationHist != nil {
		m.e2eSchedulingDurationHist = HistogramSub(m.e2eSchedulingDurationHist, sub.e2eSchedulingDurationHist)
	}
	for _, ep := range extentionsPoints {
		if sub.frameworkExtensionPointDurationHist[ep] != nil {
			m.frameworkExtensionPointDurationHist[ep] = HistogramSub(m.frameworkExtensionPointDurationHist[ep], sub.frameworkExtensionPointDurationHist[ep])
		}
	}
}

func (s *schedulerLatencyMeasurement) setQuantiles(metrics schedulerLatencyMetrics) (schedulingMetrics, error) {
	result := schedulingMetrics{
		FrameworkExtensionPointDuration: make(map[string]*measurementutil.LatencyMetric),
	}
	for _, ePoint := range extentionsPoints {
		result.FrameworkExtensionPointDuration[ePoint] = &measurementutil.LatencyMetric{}
	}

	if err := SetQuantileFromHistogram(&result.E2eSchedulingLatency, metrics.e2eSchedulingDurationHist); err != nil {
		return result, err
	}
	if err := SetQuantileFromHistogram(&result.SchedulingLatency, metrics.schedulingAlgorithmDurationHist); err != nil {
		return result, err
	}

	for _, ePoint := range extentionsPoints {
		if err := SetQuantileFromHistogram(result.FrameworkExtensionPointDuration[ePoint], metrics.frameworkExtensionPointDurationHist[ePoint]); err != nil {
			return result, err
		}
	}

	if err := SetQuantileFromHistogram(&result.PreemptionEvaluationLatency, metrics.preemptionEvaluationHist); err != nil {
		return result, err
	}
	return result, nil
}

// getSchedulingLatency retrieves scheduler latency metrics.
func (s *schedulerLatencyMeasurement) getSchedulingLatency(endpoint, token string) ([]measurement.Summary, error) {
	schedulerMetrics, err := s.getSchedulingMetrics(endpoint, token)
	if err != nil {
		return nil, err
	}
	schedulerMetrics.substract(s.initialLatency)
	result, err := s.setQuantiles(schedulerMetrics)
	if err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(schedulerLatencyMetricName, "json", content)
	return []measurement.Summary{summary}, nil
}

// getSchedulingInitialLatency retrieves initial values of scheduler latency metrics
func (s *schedulerLatencyMeasurement) getSchedulingInitialLatency(endpoint, token string) error {
	var err error
	s.initialLatency, err = s.getSchedulingMetrics(endpoint, token)
	if err != nil {
		return err
	}
	return nil
}

// getSchedulingMetrics gets scheduler latency metrics
func (s *schedulerLatencyMeasurement) getSchedulingMetrics(endpoint, token string) (schedulerLatencyMetrics, error) {
	e2eSchedulingDurationHist := measurementutil.NewHistogram(nil)
	schedulingAlgorithmDurationHist := measurementutil.NewHistogram(nil)
	preemptionEvaluationHist := measurementutil.NewHistogram(nil)
	frameworkExtensionPointDurationHist := make(map[string]*measurementutil.Histogram)
	latencyMetrics := schedulerLatencyMetrics{
		e2eSchedulingDurationHist,
		schedulingAlgorithmDurationHist,
		preemptionEvaluationHist,
		frameworkExtensionPointDurationHist}

	for _, ePoint := range extentionsPoints {
		frameworkExtensionPointDurationHist[ePoint] = measurementutil.NewHistogram(nil)
	}

	data, err := s.getMetricsViaHTTPS(endpoint, token)
	if err != nil {
		return latencyMetrics, err
	}
	samples, err := measurementutil.ExtractMetricSamples(data)
	if err != nil {
		return latencyMetrics, err
	}

	for _, sample := range samples {
		switch sample.Metric[model.MetricNameLabel] {
		case e2eSchedulingDurationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, e2eSchedulingDurationHist)
		case schedulingAlgorithmDurationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, schedulingAlgorithmDurationHist)
		case frameworkExtensionPointDurationMetricName:
			ePoint := string(sample.Metric["extension_point"])
			if _, exists := frameworkExtensionPointDurationHist[ePoint]; exists {
				measurementutil.ConvertSampleToHistogram(sample, frameworkExtensionPointDurationHist[ePoint])
			}
		case preemptionEvaluationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, preemptionEvaluationHist)
		}
	}
	return latencyMetrics, nil
}

// getMetricsViaHTTPS gets metrics using HTTPS with token authentication
func (s *schedulerLatencyMeasurement) getMetricsViaHTTPS(endpoint, token string) (string, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	client := &http.Client{
		Timeout:   singleRestCallTimeout,
		Transport: transport,
	}

	url := fmt.Sprintf("https://%s%s", endpoint, defaultMetricsPath)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// Add token for authorization if provided
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	return string(body), nil
}

// SetQuantileFromHistogram sets quantile of LatencyMetric from Histogram
func SetQuantileFromHistogram(metric *measurementutil.LatencyMetric, hist *measurementutil.Histogram) error {
	quantiles := []float64{0.5, 0.9, 0.99}
	for _, quantile := range quantiles {
		histQuantile, err := hist.Quantile(quantile)
		if err != nil {
			return err
		}
		// NaN is returned only when there are less than two buckets.
		// In which case all quantiles are NaN and all latency metrics are untouched.
		if !math.IsNaN(histQuantile) {
			metric.SetQuantile(quantile, time.Duration(int64(histQuantile*float64(time.Second))))
		}
	}

	return nil
}

type schedulingMetrics struct {
	FrameworkExtensionPointDuration map[string]*measurementutil.LatencyMetric `json:"frameworkExtensionPointDuration"`
	PreemptionEvaluationLatency     measurementutil.LatencyMetric             `json:"preemptionEvaluationLatency"`
	E2eSchedulingLatency            measurementutil.LatencyMetric             `json:"e2eSchedulingLatency"`

	// To track scheduling latency without binding, this allows to easier present the ceiling of the scheduler throughput.
	SchedulingLatency measurementutil.LatencyMetric `json:"schedulingLatency"`
}
