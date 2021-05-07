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
	v1 "k8s.io/api/core/v1"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	metricsServerLatencyName               = "MetricsServerLatency"
	metricsServerRequestDurationMetricName = model.LabelValue("apiserver_request_duration_seconds_bucket")
	metricsServerRequestDurationGroup      = model.LabelValue("metrics.k8s.io")
	metricsServerRequestDurationResource   = model.LabelValue("pods")
	metricsServerScope                     = model.LabelValue("cluster")
	metricsServerNamespace                 = "kube-system"
	metricsServerContainerName             = "metrics-server"
	portForwardLocalPort                   = 4443
)

type metricsServerLatencyMeasurement struct {
	ctx            context.Context
	cancel         func()
	isRunning      bool
	namespace      string
	forwardPort    int32
	initialLatency *measurementutil.Histogram
	wg             sync.WaitGroup
}

func init() {
	if err := measurement.Register(metricsServerLatencyName, CreateMetricsServerLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", metricsServerLatencyName, err)
	}
}

// CreateMetricsServerLatencyMeasurement creates a new
// Metrics Server Measurement.
func CreateMetricsServerLatencyMeasurement() measurement.Measurement {
	ctx, cancel := context.WithCancel(context.Background())
	return &metricsServerLatencyMeasurement{
		namespace:   metricsServerNamespace,
		forwardPort: portForwardLocalPort,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Execute supports two actions:
// - start - starts goroutine and queries /apis/metrics.k8s.io/v1beta1/pods
//   every probeIntervalDefault interval,
// it also collects initial latency metrics.
// - gather - gathers latency metrics and creates a latency summary.
func (m *metricsServerLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	if !config.CloudProvider.Features().SupportMetricsServerMetrics {
		klog.Infof("not executing metricsServerLatencyMeasurement: unsupported for provider, %s", config.ClusterFramework.GetClusterConfig().Provider.Name())
		return nil, nil
	}
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	client := config.ClusterFramework.GetClientSets().GetClient()
	kubeConf, err := clientcmd.BuildConfigFromFlags("", config.ClusterLoaderConfig.ClusterConfig.KubeConfigPath)
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if m.isRunning {
			klog.V(2).Infof("%s: measurement already running", m)
			return nil, nil
		}
		// Start executing calls towards the kubernetes' /apis/metrics.k8s.io/v1beta1/pods
		// endpoint every probeIntervalDefault until gather is called.
		// probeIntervalDefault equals the scrapping interval we suggest.
		// If we cannot get metrics for two minutes we are already going over
		// the scrape interval so we should cancel.
		m.startQuerying(m.ctx, client, probeIntervalDefault)
		// Retrieve initial latency when first call is done.
		m.initialLatency, err = m.retrieveMetricsServerLatencyMetrics(kubeConf, client)
		return nil, err
	case "gather":
		defer m.cancel()
		return m.createMetricsServerLatencySummary(kubeConf, client)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (m *metricsServerLatencyMeasurement) queryMetricsServerPodName(client clientset.Interface) (string, error) {
	podList, err := client.CoreV1().Pods(m.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "k8s-app=metrics-server"})

	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no metrics-server pod found in namespace:%s", m.namespace)
	}

	return podList.Items[0].GetName(), nil
}

func (m *metricsServerLatencyMeasurement) queryMetricsServerPort(client clientset.Interface, podName string, portName string) (int32, error) {
	metricsServerPod, err := client.CoreV1().Pods(m.namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return 0, nil
	}

	ports := make([]v1.ContainerPort, 0)

	for _, container := range metricsServerPod.Spec.Containers {
		if container.Name == metricsServerContainerName {
			ports = container.Ports
		}
	}

	for _, port := range ports {
		if port.Name == portName {
			return port.ContainerPort, nil
		}
	}

	return 0, fmt.Errorf("metrics-server pod has no ports, check manifests:%v", metricsServerPod)
}

func (m *metricsServerLatencyMeasurement) stop() error {
	if !m.isRunning {
		return fmt.Errorf("%s: measurement was not running", metricsServerLatencyName)
	}
	m.cancel()
	m.wg.Wait()
	return nil
}

// createMetricsServerLatencySummary gathers the latency one last time and creates the summary based on the Quantile from the sub histograms.
// Afterwards it creates the Summary Report.
func (m *metricsServerLatencyMeasurement) createMetricsServerLatencySummary(kubeConf *rest.Config, client clientset.Interface) ([]measurement.Summary, error) {
	latestLatency, err := m.retrieveMetricsServerLatencyMetrics(kubeConf, client)
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
	return []measurement.Summary{measurement.CreateSummary(metricsServerLatencyName, "json", content)}, nil
}

// startQuerying queries /apis/metrics.k8s.io/v1beta1/pods endpoint every interval
// and stops when stop() is called.
func (m *metricsServerLatencyMeasurement) startQuerying(ctx context.Context, client clientset.Interface, interval time.Duration) {
	m.isRunning = true
	m.wg.Add(1)
	go m.queryLoop(ctx, client, interval)
}

func (m *metricsServerLatencyMeasurement) queryLoop(ctx context.Context, client clientset.Interface, interval time.Duration) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			var output string
			output, err := m.topPods(ctx, client)
			if err != nil {
				klog.V(2).Infof("error during fetching pod resource usage: %v", err)
			}
			if output == "" {
				klog.V(2).Infof("endpoint /apis/metrics.k8s.io/v1beta1/pods returns no data")
			}
		}
	}
}

func (m *metricsServerLatencyMeasurement) retrieveMetricsServerLatencyMetrics(kubeConf *rest.Config, client clientset.Interface) (*measurementutil.Histogram, error) {
	metricsServerHist := measurementutil.NewHistogram(nil)
	output, err := m.getMetricsFromMetricsServer(kubeConf, client)
	if err != nil {
		return metricsServerHist, err
	}
	samples, err := measurementutil.ExtractMetricSamples(output)
	if err != nil {
		return metricsServerHist, err
	}
	for _, sample := range samples {
		if isMetricsServerLatencySample(sample) {
			measurementutil.ConvertSampleToHistogram(sample, metricsServerHist)
		}
	}
	return metricsServerHist, nil
}

func isMetricsServerLatencySample(sample *model.Sample) bool {
	name, nameOk := sample.Metric[model.MetricNameLabel]
	group, groupOK := sample.Metric["group"]
	resource, resourceOk := sample.Metric["resource"]
	scope, scopeOk := sample.Metric["scope"]

	if nameOk &&
		groupOK &&
		resourceOk &&
		scopeOk &&
		name == metricsServerRequestDurationMetricName &&
		group == metricsServerRequestDurationGroup &&
		resource == metricsServerRequestDurationResource &&
		scope == metricsServerScope {
		return true
	}

	return false
}

func (m *metricsServerLatencyMeasurement) getMetricsFromMetricsServer(kubeConf *rest.Config, client clientset.Interface) (string, error) {
	metricsServerPodName, err := m.queryMetricsServerPodName(client)
	if err != nil {
		return "", err
	}

	metricsServerPort, err := m.queryMetricsServerPort(client, metricsServerPodName, "https")
	if err != nil {
		return "", err
	}

	out, err := util.ProxyRequestToPod(kubeConf, m.namespace, metricsServerPodName, "https", metricsServerPort, m.forwardPort, "/metrics")
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// Dispose cleans up after the measurement.
func (m *metricsServerLatencyMeasurement) Dispose() {
	if err := m.stop(); err != nil {
		klog.V(2).Infof("error during dispose call: %v", err)
	}
}

// String returns string representation of this measurement.
func (m *metricsServerLatencyMeasurement) String() string {
	return metricsServerLatencyName
}

// topPods query /apis/metrics.k8s.io/v1beta1/pods for pods' resource usage.
func (m *metricsServerLatencyMeasurement) topPods(ctx context.Context, client clientset.Interface) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := client.CoreV1().RESTClient().Get().RequestURI("/apis/metrics.k8s.io/v1beta1/pods").Do(ctx).Raw()

	return string(out), err
}
