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
		m.initialLatency, err = m.retrieveLatencyMetrics(m.ctx, client)
		return nil, err
	case "gather":
		defer m.cancel()
		return m.createLatencySummary(m.ctx, client)
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
		m.initialLatency, err = m.retrieveLatencyMetrics(m.ctx, client)
		return nil, err
	case "gather":
		defer m.cancel()
		return m.createLatencySummary(m.ctx, client)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func getMetricsFromService(ctx context.Context, client clientset.Interface, namespace, serviceName string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := client.CoreV1().RESTClient().Get().
		Resource("services").
		SubResource("proxy").
		Namespace(namespace).
		Name(fmt.Sprintf("%v:%v", serviceName, port)).
		Suffix("metrics").
		Do(ctx).Raw()
	return string(out), err
}

func (m *ksmLatencyMeasurement) stop() error {
    if !m.isRunning {
        return fmt.Errorf("%s: measurement was not running", m)
    }
    m.isRunning = false
    m.cancel()
    m.wg.Wait()
    return nil 
}

func (m *ksmLatencyMeasurement) createLatencySummary(ctx context.Context, client clientset.Interface) ([]measurement.Summary, error) {
	latestLatency, err := m.retrieveLatencyMetrics(ctx, client)
	if err != nil {
		return nil, err
	}
	m.stop()
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
	// Create Summary.
	return []measurement.Summary{measurement.CreateSummary(ksmLatencyName, "json", content)}, nil
}

// startQuerying queries /metrics endpoint of kube-state-metrics kube_ metrics every interval
// and stops when stop is called.
func (m *ksmLatencyMeasurement) startQuerying(ctx context.Context, client clientset.Interface, interval time.Duration) {
	m.isRunning = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				_, err := getMetricsFromService(ctx, client, m.namespace, m.serviceName, m.metricsPort)
				if err != nil {
					klog.V(2).Infof("error during fetching metrics from service: %v", err)
				}
			}
		}
	}()
}

func (m *ksmLatencyMeasurement) retrieveLatencyMetrics(ctx context.Context, c clientset.Interface) (*measurementutil.Histogram, error) {
	hist := measurementutil.NewHistogram(nil)
	output, err := getMetricsFromService(ctx, c, m.namespace, m.serviceName, m.selfPort)
	if err != nil {
		return hist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		return hist, err
	}
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] == ksmRequestDurationMetricName {
			measurementutil.ConvertSampleToHistogram(sample, hist)
		}
	}
	return hist, nil
}
// Dispose cleans up after the measurement.
func (m *ksmLatencyMeasurement) Dispose() {
	if err := m.stop(); err != nil {
		klog.V(2).Infof("error during dispose call: %v", err)
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
    m.isRunning = false
    m.cancel()
    m.wg.Wait()
    return nil
}

func (m *crsmLatencyMeasurement) createLatencySummary(ctx context.Context, client clientset.Interface) ([]measurement.Summary, error) {
	latestLatency, err := m.retrieveLatencyMetrics(ctx, client)
	if err != nil {
		return nil, err
	}
	m.stop()
	finalLatency := HistogramSub(latestLatency, m.initialLatency)
	result := &measurementutil.LatencyMetric{}
	if err = SetQuantileFromHistogram(result, finalLatency); err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}
	return []measurement.Summary{measurement.CreateSummary(crsmLatencyName, "json", content)}, nil
}

func (m *crsmLatencyMeasurement) startQuerying(ctx context.Context, client clientset.Interface, interval time.Duration) {
	m.isRunning = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				_, err := getMetricsFromService(ctx, client, m.namespace, m.serviceName, m.metricsPort)
				if err != nil {
					klog.V(2).Infof("error during fetching metrics from service: %v", err)
				}
			}
		}
	}()
}

func (m *crsmLatencyMeasurement) retrieveLatencyMetrics(ctx context.Context, c clientset.Interface) (*measurementutil.Histogram, error) {
	hist := measurementutil.NewHistogram(nil)
	output, err := getMetricsFromService(ctx, c, m.namespace, m.serviceName, m.selfPort)
	if err != nil {
		return hist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		return hist, err
	}
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] == ksmRequestDurationMetricName {
			measurementutil.ConvertSampleToHistogram(sample, hist)
		}
	}
	return hist, nil
}

func (m *crsmLatencyMeasurement) Dispose() {
	if err := m.stop(); err != nil {
    	klog.V(2).Infof("error during dispose call: %v", err)
	}
}

func (m *crsmLatencyMeasurement) String() string {
	return crsmLatencyName
}