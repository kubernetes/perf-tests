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

	e2eSchedulingDurationMetricName       = model.LabelValue(schedulermetric.SchedulerSubsystem + "_e2e_scheduling_duration_seconds_bucket")
	schedulingAlgorithmDurationMetricName = model.LabelValue(schedulermetric.SchedulerSubsystem + "_scheduling_algorithm_duration_seconds_bucket")

	schedulingLatencyMetricName = model.LabelValue(schedulermetric.SchedulerSubsystem + "_" + schedulermetric.DeprecatedSchedulingDurationName)
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
	IsSSHToMasterSupported := config.ClusterFramework.GetClusterConfig().IsSSHToMasterSupported

	c := config.ClusterFramework.GetClientSets().GetClient()
	nodes, err := c.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var masterRegistered = false
	for _, node := range nodes.Items {
		if system.IsMasterNode(node.Name) {
			masterRegistered = true
		}
	}

	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return nil, err
	}

	if !IsSSHToMasterSupported || !masterRegistered {
		klog.Infof("unable to fetch scheduler metrics for provider: %s", provider)
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
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
		return nil, s.resetSchedulerMetrics(config.ClusterFramework.GetClientSets().GetClient(), masterIP, provider, masterName, masterRegistered)
	case "gather":
		klog.Infof("%s: gathering latency metrics in scheduler...", s)
		return s.getSchedulingLatency(config.ClusterFramework.GetClientSets().GetClient(), masterIP, provider, masterName, masterRegistered)
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

func (s *schedulerLatencyMeasurement) resetSchedulerMetrics(c clientset.Interface, host, provider, masterName string, masterRegistered bool) error {
	_, err := s.sendRequestToScheduler(c, "DELETE", host, provider, masterName, masterRegistered)
	if err != nil {
		return err
	}
	return nil
}

// Retrieves scheduler latency metrics.
func (s *schedulerLatencyMeasurement) getSchedulingLatency(c clientset.Interface, host, provider, masterName string, masterRegistered bool) ([]measurement.Summary, error) {
	result := schedulingMetrics{}
	data, err := s.sendRequestToScheduler(c, "GET", host, provider, masterName, masterRegistered)
	if err != nil {
		return nil, err
	}

	samples, err := measurementutil.ExtractMetricSamples(data)
	if err != nil {
		return nil, err
	}

	e2eSchedulingMetricHist := measurementutil.NewHistogram(nil)
	schedulingAlgorithmDurationHist := measurementutil.NewHistogram(nil)
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] == e2eSchedulingDurationMetricName {
			measurementutil.ConvertSampleToHistogram(sample, e2eSchedulingMetricHist)
			continue
		}
		if sample.Metric[model.MetricNameLabel] == schedulingAlgorithmDurationMetricName {
			measurementutil.ConvertSampleToHistogram(sample, schedulingAlgorithmDurationHist)
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

	if err := s.setQuantileFromHistogram(&result.E2eSchedulingLatency, e2eSchedulingMetricHist); err != nil {
		return nil, err
	}
	if err := s.setQuantileFromHistogram(&result.SchedulingLatency, schedulingAlgorithmDurationHist); err != nil {
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
func (s *schedulerLatencyMeasurement) sendRequestToScheduler(c clientset.Interface, op, host, provider, masterName string, masterRegistered bool) (string, error) {
	opUpper := strings.ToUpper(op)
	if opUpper != "GET" && opUpper != "DELETE" {
		return "", fmt.Errorf("unknown REST request")
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

	// To track scheduling latency without binding, this allows to easier present the ceiling of the scheduler throughput.
	SchedulingLatency measurementutil.LatencyMetric `json:"schedulingLatency"`
}
