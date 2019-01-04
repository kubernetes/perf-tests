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

package bundle

import (
	"fmt"

	"github.com/golang/glog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	testMetricsMeasurementName = "TestMetrics"
)

func init() {
	measurement.Register(testMetricsMeasurementName, createTestMetricsMeasurment)
}

func createTestMetricsMeasurment() measurement.Measurement {
	var metrics testMetrics
	var err error
	if metrics.etcdMetrics, err = measurement.CreateMeasurement("EtcdMetrics"); err != nil {
		glog.Errorf("%s: etcdMetrics creation error: %v", metrics, err)
	}
	if metrics.schedulingMetrics, err = measurement.CreateMeasurement("SchedulingMetrics"); err != nil {
		glog.Errorf("%s: schedulingMetrics creation error: %v", metrics, err)
	}
	if metrics.metricsForE2E, err = measurement.CreateMeasurement("MetricsForE2E"); err != nil {
		glog.Errorf("%s: metricsForE2E creation error: %v", metrics, err)
	}
	if metrics.resourceUsageSummary, err = measurement.CreateMeasurement("ResourceUsageSummary"); err != nil {
		glog.Errorf("%s: resourceUsageSummary creation error: %v", metrics, err)
	}
	if metrics.apiserverCPUProfile, err = measurement.CreateMeasurement("CPUProfile"); err != nil {
		glog.Errorf("%s: apiserverCPUProfile creation error: %v", metrics, err)
	}
	if metrics.apiserverMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		glog.Errorf("%s: apiserverMemoryProfile creation error: %v", metrics, err)
	}
	if metrics.schedulerMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		glog.Errorf("%s: schedulerMemoryProfile creation error: %v", metrics, err)
	}
	return &metrics
}

type testMetrics struct {
	etcdMetrics            measurement.Measurement
	schedulingMetrics      measurement.Measurement
	metricsForE2E          measurement.Measurement
	resourceUsageSummary   measurement.Measurement
	apiserverCPUProfile    measurement.Measurement
	apiserverMemoryProfile measurement.Measurement
	schedulerMemoryProfile measurement.Measurement
}

// Execute supports two actions. start - which sets up all metrics.
// stop - which stops all metrics and collects all measurements.
func (t *testMetrics) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	errList := errors.NewErrorList()
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}

	actionStartConfig := createConfig(config, map[string]interface{}{
		"action": "start",
	})
	actionResetConfig := createConfig(config, map[string]interface{}{
		"action": "reset",
	})
	actionGatherConfig := createConfig(config, map[string]interface{}{
		"action": "gather",
	})
	kubeApiserverConfig := createConfig(config, map[string]interface{}{
		"componentName": "kube-apiserver",
	})
	kubeSchedulerConfig := createConfig(config, map[string]interface{}{
		"componentName": "kube-scheduler",
	})

	switch action {
	case "start":
		summary, err := execute(t.etcdMetrics, actionStartConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.schedulingMetrics, actionResetConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.resourceUsageSummary, actionStartConfig)
		appendResults(&summaries, errList, summary, err)
	case "gather":
		summary, err := execute(t.etcdMetrics, actionGatherConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.schedulingMetrics, actionGatherConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.metricsForE2E, config)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.resourceUsageSummary, actionGatherConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.apiserverCPUProfile, kubeApiserverConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.apiserverMemoryProfile, kubeApiserverConfig)
		appendResults(&summaries, errList, summary, err)
		summary, err = execute(t.schedulerMemoryProfile, kubeSchedulerConfig)
		appendResults(&summaries, errList, summary, err)
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}

	if !errList.IsEmpty() {
		glog.Errorf("%s: %v", t, errList.String())
		return summaries, errList
	}
	return summaries, nil
}

// Dispose cleans up after the measurement.
func (t *testMetrics) Dispose() {
	t.etcdMetrics.Dispose()
	t.schedulingMetrics.Dispose()
	t.metricsForE2E.Dispose()
	t.resourceUsageSummary.Dispose()
	t.apiserverCPUProfile.Dispose()
	t.apiserverMemoryProfile.Dispose()
	t.schedulerMemoryProfile.Dispose()
}

// String returns a string representation of the measurement.
func (*testMetrics) String() string {
	return testMetricsMeasurementName
}

func createConfig(config *measurement.MeasurementConfig, overrides map[string]interface{}) *measurement.MeasurementConfig {
	params := make(map[string]interface{})
	for k, v := range config.Params {
		params[k] = v
	}
	for k, v := range overrides {
		params[k] = v
	}
	return &measurement.MeasurementConfig{
		ClientSets:       config.ClientSets,
		ClusterConfig:    config.ClusterConfig,
		Params:           params,
		TemplateProvider: config.TemplateProvider,
	}
}

func execute(m measurement.Measurement, config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	if m == nil {
		return nil, fmt.Errorf("uninitialized metric")
	}
	return m.Execute(config)
}

func appendResults(summaries *[]measurement.Summary, errList *errors.ErrorList, summaryResults []measurement.Summary, errResult error) {
	if errResult != nil {
		errList.Append(errResult)
	}
	*summaries = append(*summaries, summaryResults...)
}
