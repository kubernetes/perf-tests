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
	"path"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/prometheus"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	probesNamespace = "probes"

	manifestsPathPrefix = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/measurement/common/probes/manifests/"

	checkProbesReadyInterval = 15 * time.Second

	defaultCheckProbesReadyTimeout = 15 * time.Minute
)

var (
	networkLatencyConfig = proberConfig{
		Name:             "InClusterNetworkLatency",
		MetricVersion:    "v1",
		Query:            "quantile_over_time(0.99, probes:in_cluster_network_latency:histogram_quantile[%v])",
		Manifests:        "*.yaml",
		ProbeLabelValues: []string{"ping-client", "ping-server"},
	}

	dnsLookupConfig = proberConfig{
		Name:             "DnsLookupLatency",
		MetricVersion:    "v1",
		Query:            "quantile_over_time(0.99, probes:dns_lookup_latency:histogram_quantile[%v])",
		Manifests:        "dnsLookup/*yaml",
		ProbeLabelValues: []string{"dns"},
	}

	metricsServerLatencyConfig = proberConfig{
		Name:             "InClusterAPIServerRequestLatency",
		MetricVersion:    "v1",
		Query:            "quantile_over_time(0.99, probes:in_cluster_apiserver_request_latency:histogram_quantile[%v])",
		Manifests:        "metricsServer/*.yaml",
		ProbeLabelValues: []string{"metrics-server-prober"},
	}
)

func init() {
	create := func() measurement.Measurement { return createProber(networkLatencyConfig) }
	if err := measurement.Register(networkLatencyConfig.Name, create); err != nil {
		klog.Errorf("cannot register %s: %v", networkLatencyConfig.Name, err)
	}
	create = func() measurement.Measurement { return createProber(dnsLookupConfig) }
	if err := measurement.Register(dnsLookupConfig.Name, create); err != nil {
		klog.Errorf("cannot register %s: %v", dnsLookupConfig.Name, err)
	}
	create = func() measurement.Measurement { return createProber(metricsServerLatencyConfig) }
	if err := measurement.Register(metricsServerLatencyConfig.Name, create); err != nil {
		klog.Errorf("cannot register %s: %v", metricsServerLatencyConfig.Name, err)
	}
}

type proberConfig struct {
	Name             string
	MetricVersion    string
	Query            string
	Manifests        string
	ProbeLabelValues []string
}

func createProber(config proberConfig) measurement.Measurement {
	return &probesMeasurement{
		config: config,
	}
}

type probesMeasurement struct {
	config proberConfig

	framework        *framework.Framework
	replicasPerProbe int
	templateMapping  map[string]interface{}
	startTime        time.Time
}

// Execute supports two actions:
// - start - starts probes and sets up monitoring
// - gather - Gathers and prints metrics.
func (p *probesMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	if !config.CloudProvider.Features().SupportProbe {
		klog.V(1).Infof("%s: Probes cannot work in %s, skipping the measurement!", p, config.CloudProvider.Name())
		return nil, nil
	}
	if config.PrometheusFramework == nil {
		klog.Warningf("%s: Prometheus is disabled, skipping the measurement!", p)
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
		summary, err := p.gather(config.Params)
		if err != nil && !errors.IsMetricViolationError(err) {
			return nil, err
		}
		return []measurement.Summary{summary}, err
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (p *probesMeasurement) Dispose() {
	if p.framework == nil {
		klog.V(1).Infof("Probe %s wasn't started, skipping the Dispose() step", p)
		return
	}
	klog.V(2).Infof("Stopping %s probe...", p)
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
	return p.config.Name
}

func (p *probesMeasurement) initialize(config *measurement.Config) error {
	replicasPerProbe, err := util.GetInt(config.Params, "replicasPerProbe")
	if err != nil {
		return err
	}
	p.framework = config.ClusterFramework
	p.replicasPerProbe = replicasPerProbe
	p.templateMapping = map[string]interface{}{"Replicas": replicasPerProbe}
	return nil
}

func (p *probesMeasurement) start(config *measurement.Config) error {
	klog.V(2).Infof("Starting %s probe...", p)
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
	if err := p.waitForProbesReady(config); err != nil {
		return err
	}
	p.startTime = time.Now()
	return nil
}

func (p *probesMeasurement) gather(params map[string]interface{}) (measurement.Summary, error) {
	klog.V(2).Info("Gathering metrics from probes...")
	if p.startTime.IsZero() {
		return nil, fmt.Errorf("measurement %s has not been started", p)
	}
	threshold, err := util.GetDurationOrDefault(params, "threshold", 0)
	if err != nil {
		return nil, err
	}
	measurementEnd := time.Now()

	query := prepareQuery(p.config.Query, p.startTime, measurementEnd)
	pc := prom.NewInClusterPrometheusClient(p.framework.GetClientSets().GetClient())
	executor := measurementutil.NewQueryExecutor(pc)
	samples, err := executor.Query(query, measurementEnd)
	if err != nil {
		return nil, err
	}

	latency, err := measurementutil.NewLatencyMetricPrometheus(samples)
	if err != nil {
		return nil, err
	}

	var violation error
	prefix, suffix := "", ""
	if threshold > 0 {
		suffix = fmt.Sprintf(", expected perc99 <= %v", threshold)
		if err := latency.VerifyThreshold(threshold); err != nil {
			violation = errors.NewMetricViolationError(p.String(), err.Error())
			prefix = " WARNING"
		}
	}
	klog.V(2).Infof("%s:%s got %v%s", p, prefix, latency, suffix)

	summary, err := p.createSummary(*latency)
	if err != nil {
		return nil, err
	}
	return summary, violation
}

func (p *probesMeasurement) createProbesObjects() error {
	return p.framework.ApplyTemplatedManifests(path.Join(manifestsPathPrefix, p.config.Manifests), p.templateMapping)
}

func (p *probesMeasurement) waitForProbesReady(config *measurement.Config) error {
	klog.V(2).Infof("Waiting for Probe %s to become ready...", p)
	checkProbesReadyTimeout, err := util.GetDurationOrDefault(config.Params, "checkProbesReadyTimeout", defaultCheckProbesReadyTimeout)
	if err != nil {
		return err
	}
	return wait.Poll(checkProbesReadyInterval, checkProbesReadyTimeout, p.checkProbesReady)
}

func (p *probesMeasurement) checkProbesReady() (bool, error) {
	// TODO(mm4tt): Using prometheus targets to check whether probes are up is a bit hacky.
	//              Consider rewriting this to something more intuitive.
	selector := func(t prometheus.Target) bool {
		for _, value := range p.config.ProbeLabelValues {
			// NOTE(oxddr): Prometheus does some translation of labels. Labels here are not the same as labels defined on a monitored pod.
			if t.Labels["job"] == value && t.Labels["namespace"] == probesNamespace {
				return true
			}
		}
		return false
	}
	expectedTargets := p.replicasPerProbe * len(p.config.ProbeLabelValues)
	return prometheus.CheckAllTargetsReady(
		p.framework.GetClientSets().GetClient(), selector, expectedTargets)
}

func (p *probesMeasurement) createSummary(latency measurementutil.LatencyMetric) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version:   p.config.MetricVersion,
		DataItems: []measurementutil.DataItem{latency.ToPerfData(p.String())},
	})
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(p.String(), "json", content), nil
}

func prepareQuery(queryTemplate string, startTime, endTime time.Time) string {
	measurementDuration := endTime.Sub(startTime)
	return fmt.Sprintf(queryTemplate, measurementutil.ToPrometheusTime(measurementDuration))
}
