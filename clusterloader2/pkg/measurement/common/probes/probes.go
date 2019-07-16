/*
Copyright 2019 The Kubernetes Authors.

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

package probes

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/prometheus"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	probesMeasurementName = "Probes"
	probesNamespace       = "probes"

	manifestGlob = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/measurement/common/probes/manifests/*.yaml"

	checkProbesReadyInterval = 15 * time.Second
	checkProbesReadyTimeout  = 5 * time.Minute

	currentProbesMetricsVersion = "v1"
)

var (
	serviceMonitorGvr = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
)

func init() {
	if err := measurement.Register(probesMeasurementName, createProbesMeasurement); err != nil {
		klog.Errorf("cannot register %s: %v", probesMeasurementName, err)
	}
}

func createProbesMeasurement() measurement.Measurement {
	return &probesMeasurement{
		probeNameToPrometheusQueryTmpl: map[string]string{
			"in_cluster_network_latency": "quantile_over_time(0.99, probes:in_cluster_network_latency:histogram_quantile[%v])",
		},
	}
}

type probesMeasurement struct {
	// probeNameToPrometheusQueryTmpl defines a config of this measurement. Updating the config in the
	// createProbesMeasurement method is the only place in go code that needs to be changed while
	// adding a new probe.
	// Each query template should accept a single %v placeholder corresponding to the query window
	// length. See the 'in_cluster_network_latency' as an example.
	probeNameToPrometheusQueryTmpl map[string]string

	framework        *framework.Framework
	replicasPerProbe int
	templateMapping  map[string]interface{}
	startTime        time.Time
}

// Execute supports two actions:
// - start - starts probes and sets up monitoring
// - gather - Gathers and prints metrics.
func (p *probesMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	if config.CloudProvider == "kubemark" {
		klog.Info("Probes cannot work in Kubemark, skipping the measurement execution")
		return nil, nil
	}
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		return nil, p.start(config)
	case "gather":
		summaries, err := p.gather(config.Params)
		if err != nil && !errors.IsMetricViolationError(err) {
			return nil, err
		}
		return summaries, err
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (p *probesMeasurement) Dispose() {
	if p.framework == nil {
		klog.Info("Probes weren't started, skipping the Dispose() step")
		return
	}
	klog.Info("Stopping probes...")
	k8sClient := p.framework.GetClientSets().GetClient()
	if err := client.DeleteNamespace(k8sClient, probesNamespace); err != nil {
		klog.Errorf("error while deleting %s namespace: %v", probesNamespace, err)
	}
	if err := client.WaitForDeleteNamespace(k8sClient, probesNamespace); err != nil {
		klog.Errorf("error while waiting for %s namespace to be deleted: %v", probesNamespace, err)
	}
}

// String returns string representation of this measurement.
func (p *probesMeasurement) String() string {
	return probesMeasurementName
}

func (p *probesMeasurement) initialize(config *measurement.MeasurementConfig) error {
	replicasPerProbe, err := util.GetInt(config.Params, "replicasPerProbe")
	if err != nil {
		return err
	}
	p.framework = config.ClusterFramework
	p.replicasPerProbe = replicasPerProbe
	p.templateMapping = map[string]interface{}{"Replicas": replicasPerProbe}
	return nil
}

func (p *probesMeasurement) start(config *measurement.MeasurementConfig) error {
	klog.Info("Starting probes...")
	if !p.startTime.IsZero() {
		return fmt.Errorf("measurement %s cannot be started twice", p)
	}
	if err := p.initialize(config); err != nil {
		return err
	}
	k8sClient := p.framework.GetClientSets().GetClient()
	if err := client.CreateNamespace(k8sClient, probesNamespace); err != nil {
		return err
	}
	if err := p.createProbesObjects(); err != nil {
		return err
	}
	if err := p.waitForProbesReady(); err != nil {
		return err
	}
	p.startTime = time.Now()
	return nil
}

func (p *probesMeasurement) gather(params map[string]interface{}) ([]measurement.Summary, error) {
	klog.Info("Gathering metrics from probes...")
	if p.startTime.IsZero() {
		return nil, fmt.Errorf("measurement %s has not been started", p)
	}
	thresholds, err := parseThresholds(params)
	if err != nil {
		return nil, err
	}
	measurementEnd := time.Now()
	var probeSummaries []measurement.Summary
	var violationErrors []error
	for probeName, queryTmpl := range p.probeNameToPrometheusQueryTmpl {
		query := prepareQuery(queryTmpl, p.startTime, measurementEnd)
		executor := measurementutil.NewQueryExecutor(p.framework.GetClientSets().GetClient())
		samples, err := executor.Query(query, measurementEnd)
		if err != nil {
			return nil, err
		}

		latencyMetric, err := measurementutil.ParseFromPrometheus(samples)
		if err != nil {
			return nil, err
		}
		prefix, suffix := "", ""
		if threshold, ok := thresholds[probeName]; ok {
			suffix = fmt.Sprintf(", expected perc99 <= %v", threshold.Perc99)
			if err = latencyMetric.VerifyThreshold(threshold); err != nil {
				violationErrors = append(violationErrors, errors.NewMetricViolationError(probeName, err.Error()))
				prefix = " WARNING"
			}
		}
		klog.Infof("%s:%s got %v%s", probeName, prefix, latencyMetric, suffix)

		probeSummary, err := createSummary(probeName, *latencyMetric)
		if err != nil {
			return nil, err
		}
		probeSummaries = append(probeSummaries, probeSummary)
	}
	if len(violationErrors) > 0 {
		err = errors.NewMetricViolationError("probers", fmt.Sprintf("there should not be errors from probers, got %v", violationErrors))
	}
	return probeSummaries, err
}

func (p *probesMeasurement) createProbesObjects() error {
	return p.framework.ApplyTemplatedManifests(manifestGlob, p.templateMapping)
}

func (p *probesMeasurement) waitForProbesReady() error {
	klog.Info("Waiting for Probes to become ready...")
	return wait.Poll(checkProbesReadyInterval, checkProbesReadyTimeout, p.checkProbesReady)
}

func (p *probesMeasurement) checkProbesReady() (bool, error) {
	serviceMonitors, err := p.framework.GetDynamicClients().GetClient().
		Resource(serviceMonitorGvr).Namespace(probesNamespace).List(metav1.ListOptions{})
	if err != nil {
		if client.IsRetryableAPIError(err) || client.IsRetryableNetError(err) {
			err = nil // Retryable error, don't return it.
		}
		return false, err
	}
	// TODO(mm4tt): Using prometheus targets to check whether probes are up is a bit hacky.
	//              Consider rewriting this to something more intuitive.
	expectedTargets := p.replicasPerProbe * len(serviceMonitors.Items)
	return prometheus.CheckTargetsReady(
		p.framework.GetClientSets().GetClient(), isProbeTarget, expectedTargets)
}

func isProbeTarget(t prometheus.Target) bool {
	return t.Labels["namespace"] == probesNamespace
}

func parseThresholds(params map[string]interface{}) (map[string]*measurementutil.LatencyMetric, error) {
	thresholds := make(map[string]*measurementutil.LatencyMetric)
	t, ok := params["thresholds"]
	if !ok {
		return thresholds, nil
	}
	for name, thresholdVal := range t.(map[string]interface{}) {
		threshold, err := time.ParseDuration(thresholdVal.(string))
		if err != nil {
			return nil, err
		}
		thresholds[name] = makeLatencyThreshold(threshold)
	}
	return thresholds, nil
}

func makeLatencyThreshold(threshold time.Duration) *measurementutil.LatencyMetric {
	return &measurementutil.LatencyMetric{
		Perc50: threshold,
		Perc90: threshold,
		Perc99: threshold,
	}
}

func prepareQuery(queryTemplate string, startTime, endTime time.Time) string {
	measurementDuration := endTime.Sub(startTime)
	return fmt.Sprintf(queryTemplate, measurementutil.ToPrometheusTime(measurementDuration))
}

func createSummary(name string, latency measurementutil.LatencyMetric) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version:   currentProbesMetricsVersion,
		DataItems: []measurementutil.DataItem{latency.ToPerfData(probesMeasurementName)},
	})
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(name, "json", content), nil
}
