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
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	"github.com/prometheus/common/model"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	ksmLatencyName               = "KubeStateMetricsLatency"
	ksmRequestDurationMetricName = model.LabelValue("http_request_duration_seconds_bucket")
	probeIntervalDefault         = 30 * time.Second
	ksmNamespace                 = "kube-state-metrics-perf-test"
	ksmServiceName               = "kube-state-metrics"
	crsmLatencyName              = "CustomResourceStateMetricsLatency"
	crsmNamespace                = "custom-resource-state-metrics-perf-test"
	crsmServiceName              = "custom-resource-state-metrics"
	ksmSelfPort                  = 8081
	ksmMetricsPort               = 8080
)

type ksmLatencyMeasurement struct {
	ctx            context.Context
	cancel         func()
	isRunning      bool
	namespace      string
	serviceName    string
	metricsPort    int
	selfPort       int
	initialLatency *measurementutil.Histogram
	wg             sync.WaitGroup
}

type crsmLatencyMeasurement struct {
	ctx            context.Context
	cancel         func()
	isRunning      bool
	namespace      string
	serviceName    string
	metricsPort    int
	selfPort       int
	initialLatency *measurementutil.Histogram
	wg             sync.WaitGroup
}

func init() {
	if err := measurement.Register(ksmLatencyName, CreateKSMLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", ksmLatencyName, err)
	}
	if err := measurement.Register(crsmLatencyName, CreateCRSMLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", crsmLatencyName, err)
	}
}

// CreateKSMLatencyMeasurement creates a new Kube State
// Metrics Measurement.
func CreateKSMLatencyMeasurement() measurement.Measurement {
	ctx, cancel := context.WithCancel(context.Background())
	return &ksmLatencyMeasurement{
		namespace:   ksmNamespace,
		serviceName: ksmServiceName,
		selfPort:    ksmSelfPort,
		metricsPort: ksmMetricsPort,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// CreateCRSMLatencyMeasurement creates a new Custom Resource State Metrics Measurement.
func CreateCRSMLatencyMeasurement() measurement.Measurement {
	ctx, cancel := context.WithCancel(context.Background())
	return &crsmLatencyMeasurement{
		namespace:   crsmNamespace,
		serviceName: crsmServiceName,
		selfPort:    ksmSelfPort,
		metricsPort: ksmMetricsPort,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Execute supports two actions:
// - start - starts goroutine and queries /metrics every probeIntervalDefault interval,
// it also collects initial latency metrics.
// - gather - gathers latency metrics and creates a latency summary.
func (m *ksmLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	client := config.ClusterFramework.GetClientSets().GetClient()
	switch action {
	case "start":
		if m.isRunning {
			klog.V(2).Infof("%s: measurement already running", m)
			return nil, nil
		}
		// Start executing calls towards the kube-state-metrics /metrics endpoint
		// every probeIntervalDefault until gather is called.
		// probeIntervalDefault equals the scrapping interval we suggest.
		// If we cannot get metrics for two minutes we are already going over
		// the scrape interval so we should cancel.
		m.startQuerying(m.ctx, client, probeIntervalDefault)
		// Retrieve initial latency when first call is done.
		m.initialLatency, err = m.retrieveKSMLatencyMetrics(m.ctx, client)
		return nil, err
	case "gather":
		defer m.cancel()
		return m.createKSMLatencySummary(m.ctx, client)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Execute for crsmLatencyMeasurement
func (m *crsmLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	client := config.ClusterFramework.GetClientSets().GetClient()
	switch action {
	case "start":
		if m.isRunning {
			klog.V(2).Infof("%s: measurement already running", m)
			return nil, nil
		}
		m.startQuerying(m.ctx, client, probeIntervalDefault)
		m.initialLatency, err = m.retrieveCRSMLatencyMetrics(m.ctx, client)
		return nil, err
	case "gather":
		defer m.cancel()
		return m.createCRSMLatencySummary(m.ctx, client)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (m *ksmLatencyMeasurement) stop() error {
	if !m.isRunning {
		return fmt.Errorf("%s: measurement was not running", m)
	}
	m.cancel()
	m.wg.Wait()
	return nil
}

func (m *ksmLatencyMeasurement) createKSMLatencySummary(ctx context.Context, client clientset.Interface) ([]measurement.Summary, error) {
	latestLatency, err := m.retrieveKSMLatencyMetrics(ctx, client)
	if err != nil {
		return nil, err
	}
	if err = m.stop(); err != nil {
		return nil, err
	}
	// We want to subtract the latest histogram from the first one we collect.
	finalLatency := HistogramSub(latestLatency, m.initialLatency)
	// Pretty Print the report.
	result := &measurementutil.LatencyMetric{}
	if err = SetQuantileFromHistogram(result, finalLatency); err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}
	
	// Log the latency results for visibility
	klog.Infof("%s: Latency Results - P50: %v, P90: %v, P99: %v", 
		m, result.Perc50, result.Perc90, result.Perc99)
	klog.Infof("%s: Final latency summary: %s", m, content)
	
	// Create Summary.
	return []measurement.Summary{measurement.CreateSummary(ksmLatencyName, "json", content)}, nil
}

// startQuerying queries /metrics endpoint of kube-state-metrics kube_ metrics every interval
// and stops when stop is called.
func (m *ksmLatencyMeasurement) startQuerying(ctx context.Context, client clientset.Interface, interval time.Duration) {
	m.isRunning = true
	m.wg.Add(1)
	go m.queryLoop(ctx, client, interval)
}

func (m *ksmLatencyMeasurement) queryLoop(ctx context.Context, client clientset.Interface, interval time.Duration) {
	defer m.wg.Done()
	queryCount := 0
	for {
		select {
		case <-ctx.Done():
			klog.V(2).Infof("%s: stopping query loop after %d queries", m, queryCount)
			return
		case <-time.After(interval):
			queryCount++
			klog.V(4).Infof("%s: executing query #%d", m, queryCount)
			output, err := m.getMetricsFromService(ctx, client, m.metricsPort)
			if err != nil {
				klog.V(2).Infof("%s: error during fetching metrics from service (query #%d): %v", m, queryCount, err)
			}
			if output == "" {
				klog.V(2).Infof("%s: /metrics endpoint returned no data in namespace: %s from service: %s port: %d", 
					m, m.namespace, m.serviceName, m.metricsPort)
			} else {
				klog.V(4).Infof("%s: successfully fetched %d bytes from metrics endpoint (query #%d)", 
					m, len(output), queryCount)
			}
		}
	}
}

func (m *ksmLatencyMeasurement) retrieveKSMLatencyMetrics(ctx context.Context, c clientset.Interface) (*measurementutil.Histogram, error) {
	klog.V(4).Infof("%s: retrieving KSM latency metrics", m)
	ksmHist := measurementutil.NewHistogram(nil)
	output, err := m.getMetricsFromService(ctx, c, m.selfPort)
	if err != nil {
		klog.Errorf("%s: failed to get metrics from service: %v", m, err)
		return ksmHist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		klog.Errorf("%s: failed to extract metric samples: %v", m, err)
		return ksmHist, err
	}
	
	sampleCount := 0
	for _, sample := range samples {
		switch sample.Metric[model.MetricNameLabel] {
		case ksmRequestDurationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, ksmHist)
			sampleCount++
		}
	}
	klog.V(4).Infof("%s: processed %d histogram samples", m, sampleCount)
	return ksmHist, nil
}

func (m *ksmLatencyMeasurement) getMetricsFromService(ctx context.Context, client clientset.Interface, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	
	klog.V(4).Infof("%s: fetching metrics from %s/%s:%d", m, m.namespace, m.serviceName, port)
	
	out, err := client.CoreV1().RESTClient().Get().
		Resource("services").
		SubResource("proxy").
		Namespace(m.namespace).
		Name(fmt.Sprintf("%v:%v", m.serviceName, port)).
		Suffix("metrics").
		Do(ctx).Raw()
	
	if err != nil {
		klog.V(2).Infof("%s: error fetching metrics from %s/%s:%d: %v", m, m.namespace, m.serviceName, port, err)
	}
	
	return string(out), err
}

// Dispose cleans up after the measurement.
func (m *ksmLatencyMeasurement) Dispose() {
	if err := m.stop(); err != nil {
		klog.V(2).Infof("%s: error during dispose call: %v", m, err)
	}
}

// String returns string representation of this measurement.
func (m *ksmLatencyMeasurement) String() string {
	return ksmLatencyName
}

func (m *crsmLatencyMeasurement) stop() error {
	if !m.isRunning {
		return fmt.Errorf("%s: measurement was not running", m)
	}
	m.cancel()
	m.wg.Wait()
	return nil
}

func (m *crsmLatencyMeasurement) createCRSMLatencySummary(ctx context.Context, client clientset.Interface) ([]measurement.Summary, error) {
	latestLatency, err := m.retrieveCRSMLatencyMetrics(ctx, client)
	if err != nil {
		return nil, err
	}
	if err = m.stop(); err != nil {
		return nil, err
	}
	finalLatency := HistogramSub(latestLatency, m.initialLatency)
	result := &measurementutil.LatencyMetric{}
	if err = SetQuantileFromHistogram(result, finalLatency); err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}
	
	// Log the latency results for visibility
	klog.Infof("%s: Latency Results - P50: %v, P90: %v, P99: %v", 
		m, result.Perc50, result.Perc90, result.Perc99)
	klog.Infof("%s: Final latency summary: %s", m, content)
	
	return []measurement.Summary{measurement.CreateSummary(crsmLatencyName, "json", content)}, nil
}

func (m *crsmLatencyMeasurement) startQuerying(ctx context.Context, client clientset.Interface, interval time.Duration) {
	m.isRunning = true
	m.wg.Add(1)
	go m.queryLoop(ctx, client, interval)
}

func (m *crsmLatencyMeasurement) queryLoop(ctx context.Context, client clientset.Interface, interval time.Duration) {
	defer m.wg.Done()
	queryCount := 0
	for {
		select {
		case <-ctx.Done():
			klog.V(2).Infof("%s: stopping query loop after %d queries", m, queryCount)
			return
		case <-time.After(interval):
			queryCount++
			klog.V(4).Infof("%s: executing query #%d", m, queryCount)
			output, err := m.getMetricsFromService(ctx, client, m.metricsPort)
			if err != nil {
				klog.V(2).Infof("%s: error during fetching metrics from service (query #%d): %v", m, queryCount, err)
			}
			if output == "" {
				klog.V(2).Infof("%s: /metrics endpoint returned no data in namespace: %s from service: %s port: %d", 
					m, m.namespace, m.serviceName, m.metricsPort)
			} else {
				klog.V(4).Infof("%s: successfully fetched %d bytes from metrics endpoint (query #%d)", 
					m, len(output), queryCount)
			}
		}
	}
}

func (m *crsmLatencyMeasurement) retrieveCRSMLatencyMetrics(ctx context.Context, c clientset.Interface) (*measurementutil.Histogram, error) {
	klog.V(4).Infof("%s: retrieving CRSM latency metrics", m)
	crsmHist := measurementutil.NewHistogram(nil)
	output, err := m.getMetricsFromService(ctx, c, m.selfPort)
	if err != nil {
		klog.Errorf("%s: failed to get metrics from service: %v", m, err)
		return crsmHist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		klog.Errorf("%s: failed to extract metric samples: %v", m, err)
		return crsmHist, err
	}
	
	sampleCount := 0
	for _, sample := range samples {
		switch sample.Metric[model.MetricNameLabel] {
		case ksmRequestDurationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, crsmHist)
			sampleCount++
		}
	}
	klog.V(4).Infof("%s: processed %d histogram samples", m, sampleCount)
	return crsmHist, nil
}

func (m *crsmLatencyMeasurement) getMetricsFromService(ctx context.Context, client clientset.Interface, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	
	klog.V(4).Infof("%s: fetching metrics from %s/%s:%d", m, m.namespace, m.serviceName, port)
	
	out, err := client.CoreV1().RESTClient().Get().
		Resource("services").
		SubResource("proxy").
		Namespace(m.namespace).
		Name(fmt.Sprintf("%v:%v", m.serviceName, port)).
		Suffix("metrics").
		Do(ctx).Raw()
	
	if err != nil {
		klog.V(2).Infof("%s: error fetching metrics from %s/%s:%d: %v", m, m.namespace, m.serviceName, port, err)
	}
	
	return string(out), err
}

func (m *crsmLatencyMeasurement) Dispose() {
	if err := m.stop(); err != nil {
		klog.V(2).Infof("%s: error during dispose call: %v", m, err)
	}
}

func (m *crsmLatencyMeasurement) String() string {
	return crsmLatencyName
}