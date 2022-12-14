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

	goerrors "github.com/go-errors/errors"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	testMetricsMeasurementName = "TestMetrics"
)

func init() {
	if err := measurement.Register(testMetricsMeasurementName, createTestMetricsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", testMetricsMeasurementName, err)
	}
}

func createTestMetricsMeasurement() measurement.Measurement {
	var metrics testMetrics
	var err error
	if metrics.etcdMetrics, err = measurement.CreateMeasurement("EtcdMetrics"); err != nil {
		klog.Errorf("%v: etcdMetrics creation error: %v", metrics, err)
	}
	if metrics.schedulingMetrics, err = measurement.CreateMeasurement("SchedulingMetrics"); err != nil {
		klog.Errorf("%v: schedulingMetrics creation error: %v", metrics, err)
	}
	if metrics.metricsForE2E, err = measurement.CreateMeasurement("MetricsForE2E"); err != nil {
		klog.Errorf("%v: metricsForE2E creation error: %v", metrics, err)
	}
	if metrics.resourceUsageSummary, err = measurement.CreateMeasurement("ResourceUsageSummary"); err != nil {
		klog.Errorf("%v: resourceUsageSummary creation error: %v", metrics, err)
	}
	if metrics.etcdCPUProfile, err = measurement.CreateMeasurement("CPUProfile"); err != nil {
		klog.Errorf("%v: etcdCPUProfile creation error: %v", metrics, err)
	}
	if metrics.etcdMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		klog.Errorf("%v: etcdMemoryProfile creation error: %v", metrics, err)
	}
	if metrics.etcdBlockProfile, err = measurement.CreateMeasurement("BlockProfile"); err != nil {
		klog.Errorf("%v: etcdBlockProfile creation error: %v", metrics, err)
	}
	if metrics.apiserverCPUProfile, err = measurement.CreateMeasurement("CPUProfile"); err != nil {
		klog.Errorf("%v: apiserverCPUProfile creation error: %v", metrics, err)
	}
	if metrics.apiserverMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		klog.Errorf("%v: apiserverMemoryProfile creation error: %v", metrics, err)
	}
	if metrics.apiserverBlockProfile, err = measurement.CreateMeasurement("BlockProfile"); err != nil {
		klog.Errorf("%v: apiserverBlockProfile creation error: %v", metrics, err)
	}
	if metrics.schedulerCPUProfile, err = measurement.CreateMeasurement("CPUProfile"); err != nil {
		klog.Errorf("%v: schedulerCPUProfile creation error: %v", metrics, err)
	}
	if metrics.schedulerMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		klog.Errorf("%v: schedulerMemoryProfile creation error: %v", metrics, err)
	}
	if metrics.schedulerBlockProfile, err = measurement.CreateMeasurement("BlockProfile"); err != nil {
		klog.Errorf("%v: schedulerBlockProfile creation error: %v", metrics, err)
	}
	if metrics.controllerManagerCPUProfile, err = measurement.CreateMeasurement("CPUProfile"); err != nil {
		klog.Errorf("%v: controllerManagerCPUProfile creation error: %v", metrics, err)
	}
	if metrics.controllerManagerMemoryProfile, err = measurement.CreateMeasurement("MemoryProfile"); err != nil {
		klog.Errorf("%v: controllerManagerMemoryProfile creation error: %v", metrics, err)
	}
	if metrics.controllerManagerBlockProfile, err = measurement.CreateMeasurement("BlockProfile"); err != nil {
		klog.Errorf("%v: controllerManagerBlockProfile creation error: %v", metrics, err)
	}
	if metrics.systemPodMetrics, err = measurement.CreateMeasurement("SystemPodMetrics"); err != nil {
		klog.Errorf("%v: systemPodMetrics creation error: %v", metrics, err)
	}
	if metrics.clusterOOMsTracker, err = measurement.CreateMeasurement("ClusterOOMsTracker"); err != nil {
		klog.Errorf("%v: clusterOOMsTracker creation error: %v", metrics, err)
	}
	return &metrics
}

type testMetrics struct {
	etcdMetrics                    measurement.Measurement
	schedulingMetrics              measurement.Measurement
	metricsForE2E                  measurement.Measurement
	resourceUsageSummary           measurement.Measurement
	etcdCPUProfile                 measurement.Measurement
	etcdMemoryProfile              measurement.Measurement
	etcdBlockProfile               measurement.Measurement
	apiserverCPUProfile            measurement.Measurement
	apiserverMemoryProfile         measurement.Measurement
	apiserverBlockProfile          measurement.Measurement
	schedulerCPUProfile            measurement.Measurement
	schedulerMemoryProfile         measurement.Measurement
	schedulerBlockProfile          measurement.Measurement
	controllerManagerCPUProfile    measurement.Measurement
	controllerManagerMemoryProfile measurement.Measurement
	controllerManagerBlockProfile  measurement.Measurement
	systemPodMetrics               measurement.Measurement
	clusterOOMsTracker             measurement.Measurement
}

// Execute supports two actions. start - which sets up all metrics.
// stop - which stops all metrics and collects all measurements.
func (t *testMetrics) Execute(config *measurement.Config) ([]measurement.Summary, error) {
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
	etcdStartConfig := createConfig(config, map[string]interface{}{
		"action":        "start",
		"componentName": "etcd",
	})
	etcdGatherConfig := createConfig(config, map[string]interface{}{
		"action":        "gather",
		"componentName": "etcd",
	})
	kubeApiserverStartConfig := createConfig(config, map[string]interface{}{
		"action":        "start",
		"componentName": "kube-apiserver",
	})
	kubeApiserverGatherConfig := createConfig(config, map[string]interface{}{
		"action":        "gather",
		"componentName": "kube-apiserver",
	})
	kubeSchedulerStartConfig := createConfig(config, map[string]interface{}{
		"action":        "start",
		"componentName": "kube-scheduler",
	})
	kubeSchedulerGatherConfig := createConfig(config, map[string]interface{}{
		"action":        "gather",
		"componentName": "kube-scheduler",
	})
	kubeControllerManagerStartConfig := createConfig(config, map[string]interface{}{
		"action":        "start",
		"componentName": "kube-controller-manager",
	})
	kubeControllerManagerGatherConfig := createConfig(config, map[string]interface{}{
		"action":        "gather",
		"componentName": "kube-controller-manager",
	})

	switch action {
	case "start":
		summary, err := execute(t.etcdMetrics, actionStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdMetrics.String(), action, err))
		summary, err = execute(t.schedulingMetrics, actionResetConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulingMetrics.String(), action, err))
		summary, err = execute(t.resourceUsageSummary, actionStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.resourceUsageSummary.String(), action, err))
		summary, err = execute(t.etcdCPUProfile, etcdStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdCPUProfile.String(), action, err))
		summary, err = execute(t.etcdMemoryProfile, etcdStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdMemoryProfile.String(), action, err))
		summary, err = execute(t.etcdBlockProfile, etcdStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdBlockProfile.String(), action, err))
		summary, err = execute(t.apiserverCPUProfile, kubeApiserverStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverCPUProfile.String(), action, err))
		summary, err = execute(t.apiserverMemoryProfile, kubeApiserverStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverMemoryProfile.String(), action, err))
		summary, err = execute(t.apiserverBlockProfile, kubeApiserverStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverBlockProfile.String(), action, err))
		summary, err = execute(t.schedulerCPUProfile, kubeSchedulerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerCPUProfile.String(), action, err))
		summary, err = execute(t.schedulerMemoryProfile, kubeSchedulerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerMemoryProfile.String(), action, err))
		summary, err = execute(t.schedulerBlockProfile, kubeSchedulerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerBlockProfile.String(), action, err))
		summary, err = execute(t.controllerManagerCPUProfile, kubeControllerManagerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerCPUProfile.String(), action, err))
		summary, err = execute(t.controllerManagerMemoryProfile, kubeControllerManagerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerMemoryProfile.String(), action, err))
		summary, err = execute(t.controllerManagerBlockProfile, kubeControllerManagerStartConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerBlockProfile.String(), action, err))
		summary, err = execute(t.systemPodMetrics, config)
		appendResults(&summaries, errList, summary, executeError(t.systemPodMetrics.String(), action, err))
		summary, err = execute(t.clusterOOMsTracker, config)
		appendResults(&summaries, errList, summary, executeError(t.clusterOOMsTracker.String(), action, err))
	case "gather":
		summary, err := execute(t.etcdMetrics, actionGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdMetrics.String(), action, err))
		summary, err = execute(t.schedulingMetrics, actionGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulingMetrics.String(), action, err))
		summary, err = execute(t.metricsForE2E, config)
		appendResults(&summaries, errList, summary, executeError(t.metricsForE2E.String(), action, err))
		summary, err = execute(t.resourceUsageSummary, actionGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.resourceUsageSummary.String(), action, err))
		summary, err = execute(t.etcdCPUProfile, etcdGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdCPUProfile.String(), action, err))
		summary, err = execute(t.etcdMemoryProfile, etcdGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdMemoryProfile.String(), action, err))
		summary, err = execute(t.etcdBlockProfile, etcdGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.etcdBlockProfile.String(), action, err))
		summary, err = execute(t.apiserverCPUProfile, kubeApiserverGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverCPUProfile.String(), action, err))
		summary, err = execute(t.apiserverMemoryProfile, kubeApiserverGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverMemoryProfile.String(), action, err))
		summary, err = execute(t.apiserverBlockProfile, kubeApiserverGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.apiserverBlockProfile.String(), action, err))
		summary, err = execute(t.schedulerCPUProfile, kubeSchedulerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerCPUProfile.String(), action, err))
		summary, err = execute(t.schedulerMemoryProfile, kubeSchedulerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerMemoryProfile.String(), action, err))
		summary, err = execute(t.schedulerBlockProfile, kubeSchedulerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.schedulerBlockProfile.String(), action, err))
		summary, err = execute(t.controllerManagerCPUProfile, kubeControllerManagerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerCPUProfile.String(), action, err))
		summary, err = execute(t.controllerManagerMemoryProfile, kubeControllerManagerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerMemoryProfile.String(), action, err))
		summary, err = execute(t.controllerManagerBlockProfile, kubeControllerManagerGatherConfig)
		appendResults(&summaries, errList, summary, executeError(t.controllerManagerBlockProfile.String(), action, err))
		summary, err = execute(t.systemPodMetrics, config)
		appendResults(&summaries, errList, summary, executeError(t.systemPodMetrics.String(), action, err))
		summary, err = execute(t.clusterOOMsTracker, config)
		appendResults(&summaries, errList, summary, executeError(t.clusterOOMsTracker.String(), action, err))
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}

	if !errList.IsEmpty() {
		klog.Errorf("%s: %v", t, errList.String())
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
	t.etcdCPUProfile.Dispose()
	t.etcdMemoryProfile.Dispose()
	t.etcdBlockProfile.Dispose()
	t.apiserverCPUProfile.Dispose()
	t.apiserverMemoryProfile.Dispose()
	t.apiserverBlockProfile.Dispose()
	t.schedulerCPUProfile.Dispose()
	t.schedulerMemoryProfile.Dispose()
	t.schedulerBlockProfile.Dispose()
	t.controllerManagerCPUProfile.Dispose()
	t.controllerManagerMemoryProfile.Dispose()
	t.controllerManagerBlockProfile.Dispose()
}

// String returns a string representation of the measurement.
func (*testMetrics) String() string {
	return testMetricsMeasurementName
}

func createConfig(config *measurement.Config, overrides map[string]interface{}) *measurement.Config {
	params := make(map[string]interface{})
	for k, v := range config.Params {
		params[k] = v
	}
	for k, v := range overrides {
		params[k] = v
	}
	return &measurement.Config{
		ClusterFramework:    config.ClusterFramework,
		PrometheusFramework: config.PrometheusFramework,
		Params:              params,
		TemplateProvider:    config.TemplateProvider,
		CloudProvider:       config.CloudProvider,
	}
}

func execute(m measurement.Measurement, config *measurement.Config) ([]measurement.Summary, error) {
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

func executeError(measurement, action string, err error) error {
	if err != nil {
		return goerrors.Errorf("action %s failed for %s measurement: %v", action, measurement, err)
	}
	return nil
}
