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
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/master/ports"
	schedulermetric "k8s.io/kubernetes/pkg/scheduler/metrics"
	"k8s.io/kubernetes/pkg/util/system"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulerLatencyMetricName = "SchedulingMetrics"

	e2eSchedulingMetricName = "scheduler_e2e_scheduling_duration_seconds_bucket"

	schedulingLatencyMetricName = model.LabelValue(schedulermetric.SchedulerSubsystem + "_" + schedulermetric.SchedulingLatencyName)
	singleRestCallTimeout       = 5 * time.Minute
)

func init() {
	if err := measurement.Register(schedulerLatencyMetricName, createSchedulerLatencyMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", schedulerLatencyMetricName, err)
	}
}

func createSchedulerLatencyMeasurement() measurement.Measurement {
	return &schedulerLatencyMeasurement{}
}

type schedulerLatencyMeasurement struct{}

// Execute supports two actions:
// - reset - Resets latency data on api scheduler side.
// - gather - Gathers and prints current scheduler latency data.
func (s *schedulerLatencyMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return nil, err
	}
	masterIP, err := util.GetStringOrDefault(config.Params, "masterIP", config.ClusterFramework.GetClusterConfig().GetMasterIp())
	if err != nil {
		return nil, err
	}
	masterName, err := util.GetStringOrDefault(config.Params, "masterName", config.ClusterFramework.GetClusterConfig().MasterName)
	if err != nil {
		return nil, err
	}

	switch action {
	case "reset":
		klog.Infof("%s: resetting latency metrics in scheduler...", s)
		return nil, s.resetSchedulerMetrics(config.ClusterFramework.GetClientSets().GetClient(), masterIP, provider, masterName)
	case "gather":
		klog.Infof("%s: gathering latency metrics in scheduler...", s)
		return s.getSchedulingLatency(config.ClusterFramework.GetClientSets().GetClient(), masterIP, provider, masterName)
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

func (s *schedulerLatencyMeasurement) resetSchedulerMetrics(c clientset.Interface, host, provider, masterName string) error {
	_, err := s.sendRequestToScheduler(c, "DELETE", host, provider, masterName)
	if err != nil {
		return err
	}
	return nil
}

// Retrieves scheduler latency metrics.
func (s *schedulerLatencyMeasurement) getSchedulingLatency(c clientset.Interface, host, provider, masterName string) ([]measurement.Summary, error) {
	result := schedulingMetrics{}
	data, err := s.sendRequestToScheduler(c, "GET", host, provider, masterName)
	if err != nil {
		return nil, err
	}

	result.Str = data

	samples, err := measurementutil.ExtractMetricSamples(data)
	if err != nil {
		return nil, err
	}

	hist := measurementutil.NewHistogram(nil)
	for _, sample := range samples {
		result.Str += fmt.Sprintf("\nsample: %#v\n", sample)
		if sample.Metric[model.MetricNameLabel] == e2eSchedulingMetricName {
			measurementutil.ConvertSampleToHistogram(sample, hist)
			continue
		}
		if sample.Metric[model.MetricNameLabel] != schedulingLatencyMetricName {
			continue
		}

		var metric *measurementutil.LatencyMetric
		switch sample.Metric[schedulermetric.OperationLabel] {
		case schedulermetric.PredicateEvaluation:
			metric = &result.PredicateEvaluationLatency
		case schedulermetric.PriorityEvaluation:
			metric = &result.PriorityEvaluationLatency
		case schedulermetric.PreemptionEvaluation:
			metric = &result.PreemptionEvaluationLatency
		case schedulermetric.Binding:
			metric = &result.BindingLatency
		}

		if metric == nil {
			continue
		}

		quantile, err := strconv.ParseFloat(string(sample.Metric[model.QuantileLabel]), 64)
		if err != nil {
			return nil, err
		}
		metric.SetQuantile(quantile, time.Duration(int64(float64(sample.Value)*float64(time.Second))))
	}

	if err := s.setQuantileFromHistogram(&result.E2eSchedulingLatency, hist); err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(result)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(schedulerLatencyMetricName, "json", content)
	return []measurement.Summary{summary}, nil
}

// Set quantile of LatencyMetric from Histogram
func (s *schedulerLatencyMeasurement) setQuantileFromHistogram(metric *measurementutil.LatencyMetric, hist *measurementutil.Histogram) error {
	quantiles := []float64{0.5, 0.9, 0.99}
	for _, quantile := range quantiles {
		histQuantile, err := hist.Quantile(quantile)
		if err != nil {
			return err
		}
		metric.SetQuantile(quantile, time.Duration(int64(histQuantile*float64(time.Second))))
	}

	return nil
}

// Sends request to kube scheduler metrics
func (s *schedulerLatencyMeasurement) sendRequestToScheduler(c clientset.Interface, op, host, provider, masterName string) (string, error) {
	opUpper := strings.ToUpper(op)
	if opUpper != "GET" && opUpper != "DELETE" {
		return "", fmt.Errorf("unknown REST request")
	}

	nodes, err := c.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	var masterRegistered = false
	for _, node := range nodes.Items {
		if system.IsMasterNode(node.Name) {
			masterRegistered = true
		}
	}

	var responseText string
	if masterRegistered {
		ctx, cancel := context.WithTimeout(context.Background(), singleRestCallTimeout)
		defer cancel()

		body, err := c.CoreV1().RESTClient().Verb(opUpper).
			Context(ctx).
			Namespace(metav1.NamespaceSystem).
			Resource("pods").
			Name(fmt.Sprintf("kube-scheduler-%v:%v", masterName, ports.InsecureSchedulerPort)).
			SubResource("proxy").
			Suffix("metrics").
			Do().Raw()

		if err != nil {
			klog.Errorf("Send request to scheduler failed with err: %v", err)
			return "", err
		}
		responseText = string(body)
	} else {
		// If master is not registered fall back to old method of using SSH.
		if provider == "gke" {
			klog.Infof("%s: not grabbing scheduler metrics through master SSH: unsupported for gke", s)
			return "", nil
		}

		cmd := "curl -X " + opUpper + " http://localhost:10251/metrics"
		sshResult, err := measurementutil.SSH(cmd, host+":22", provider)
		if err != nil || sshResult.Code != 0 {
			return "", fmt.Errorf("unexpected error (code: %d) in ssh connection to master: %#v", sshResult.Code, err)
		}
		responseText = sshResult.Stdout
	}
	return responseText, nil
}

type schedulingMetrics struct {
	PredicateEvaluationLatency  measurementutil.LatencyMetric `json:"predicateEvaluationLatency"`
	PriorityEvaluationLatency   measurementutil.LatencyMetric `json:"priorityEvaluationLatency"`
	PreemptionEvaluationLatency measurementutil.LatencyMetric `json:"preemptionEvaluationLatency"`
	BindingLatency              measurementutil.LatencyMetric `json:"bindingLatency"`
	E2eSchedulingLatency        measurementutil.LatencyMetric `json:"e2eSchedulingLatency"`
	Str                         string                        `json:"str"`
}
