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
	"k8s.io/klog"
)

const (
	ksmLatencyName               = "KubeStateMetricsLatency"
	ksmRequestDurationMetricName = model.LabelValue("http_request_duration_seconds_bucket")
	probeIntervalDefault         = 30 * time.Second
	ksmNamespace                 = "kube-state-metrics-perf-test"
	ksmServiceName               = "kube-state-metrics"
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

func init() {
	if err := measurement.Register(ksmLatencyName, CreateKSMLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", ksmLatencyName, err)
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

// Execute supports two actions:
// - start - starts goroutine and queries /metrics every probeIntervalDefault interval,
// it also collects initial latency metrics.
// - gather - gathers latency metrics and creates a latency summary.
func (m *ksmLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	if !config.CloudProvider.Features().SupportKubeStateMetrics {
		klog.Infof("not executing KSMLatencyMeasurement: unsupported for provider, %s", config.ClusterFramework.GetClusterConfig().Provider.Name())
		return nil, nil
	}
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

func (m *ksmLatencyMeasurement) stop() error {
	if !m.isRunning {
		return fmt.Errorf("%s: measurement was not running", m)
	}
	m.cancel()
	m.wg.Wait()
	return nil
}

// createKSMLatencyReport gathers the latency one last time and creates the summary based on the Quantile from the sub histograms.
// Afterwards it creates the Summary Report.
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			var output string
			output, err := m.getMetricsFromService(ctx, client, m.metricsPort)
			if err != nil {
				klog.V(2).Infof("error during fetching metrics from service: %v", err)
			}
			if output == "" {
				klog.V(2).Infof("/metrics endpoint of kube-state-metrics returned no data in namespace: %s from service: %s port: %d", m.namespace, m.serviceName, m.metricsPort)
			}

		}
	}
}

func (m *ksmLatencyMeasurement) retrieveKSMLatencyMetrics(ctx context.Context, c clientset.Interface) (*measurementutil.Histogram, error) {
	ksmHist := measurementutil.NewHistogram(nil)
	output, err := m.getMetricsFromService(ctx, c, m.selfPort)
	if err != nil {
		return ksmHist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		return ksmHist, err
	}
	for _, sample := range samples {
		switch sample.Metric[model.MetricNameLabel] {
		case ksmRequestDurationMetricName:
			measurementutil.ConvertSampleToHistogram(sample, ksmHist)
		}
	}
	return ksmHist, nil
}

func (m *ksmLatencyMeasurement) getMetricsFromService(ctx context.Context, client clientset.Interface, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := client.CoreV1().RESTClient().Get().
		Resource("services").
		SubResource("proxy").
		Namespace(m.namespace).
		Name(fmt.Sprintf("%v:%v", m.serviceName, port)).
		Suffix("metrics").
		Do(ctx).Raw()
	return string(out), err
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
