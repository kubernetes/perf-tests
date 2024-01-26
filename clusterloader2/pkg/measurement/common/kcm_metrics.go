/*
Copyright 2024 The Kubernetes Authors.

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
	"fmt"
	"math"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	kcmWorkQueueDepthMetricName    = "KCMWorkQueueDepthMetrics"
	kcmWorkQueueDepthMetricVersion = "v1"
	hpaResource                    = "horizontalpodautoscaler"
)

func init() {
	if err := measurement.Register(kcmWorkQueueDepthMetricName, createKCMWorkQueueDepthMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", kcmWorkQueueDepthMetricName, err)
	}
}

func createKCMWorkQueueDepthMeasurement() measurement.Measurement {
	return &kcmWorkQueueDepthMeasurement{
		depthMetrics: map[string]float64{},
		stopCh:       make(chan struct{}),
	}
}

type kcmWorkQueueDepthMeasurement struct {
	// depthMetrics is the minimum workqueue depth for each resouce in the leader.
	depthMetrics      map[string]float64
	hpaAllowedBacklog *float64

	isRunning bool
	stopCh    chan struct{}
}

func (m *kcmWorkQueueDepthMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	provider := config.ClusterFramework.GetClusterConfig().Provider
	hosts := config.ClusterFramework.GetClusterConfig().MasterIPs
	SSHToMasterSupported := provider.Features().SupportSSHToMaster

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	kcmPort := config.ClusterFramework.GetClusterConfig().KCMPort
	switch action {
	case "start":
		klog.V(2).Infof("%s: starting kcm metrics collecting...", m)
		if !SSHToMasterSupported {
			klog.Warningf("ssh to master node is not supported for provider: %v", provider.Name())
			return nil, nil
		}
		m.start(provider, hosts, kcmPort)
		return nil, nil
	case "gather":
		m.Dispose()
		hpaAllowedBacklog, err := util.GetFloat64OrDefault(config.Params, "hpaAllowedBacklog", -1)
		if err != nil {
			return nil, err
		}
		if hpaAllowedBacklog >= 0 {
			m.hpaAllowedBacklog = &hpaAllowedBacklog
		}
		summary, err := m.createSummary()
		return []measurement.Summary{summary}, err
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (m *kcmWorkQueueDepthMeasurement) Dispose() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
	}
}

// String returns string representation of this measurement.
func (m *kcmWorkQueueDepthMeasurement) String() string {
	return kcmWorkQueueDepthMetricName
}

func (m *kcmWorkQueueDepthMeasurement) start(provider provider.Provider, hosts []string, port int) error {
	if m.isRunning {
		return fmt.Errorf("%s: measurement already running", m)
	}
	m.isRunning = true
	klog.V(3).Infof("collecting KCM workqueue depth metrics from hosts: %v", hosts)
	if len(hosts) == 0 {
		return nil
	}

	// There's only one goroutine that collect metrics from all KCM instances.
	go func() {
		for {
			select {
			case <-time.After(time.Second):
				metricsFromHosts := []map[string]float64{}
				for _, host := range hosts {
					workQueueDepthMetrics, err := m.getKCMWorkQueueDepth(host, provider, port)
					if err != nil {
						klog.Errorf("%s: failed to collect KCM workqueue depth: %v", m, err)
						continue
					}
					metricsFromHosts = append(metricsFromHosts, workQueueDepthMetrics)
				}
				if len(metricsFromHosts) == 0 {
					continue
				}
				// For one single data point in each KCM instance, we take the max,
				// since the leader will have larger workqueue backlog.
				result := metricsFromHosts[0]
				for i := 1; i < len(metricsFromHosts); i++ {
					result = getMaxOrMin(result, metricsFromHosts[i], true)
				}
				// We keep tracking the smallest value that we have seen.
				m.depthMetrics = getMaxOrMin(m.depthMetrics, result, false)
			case <-m.stopCh:
				return
			}
		}
	}()
	return nil
}

func (m *kcmWorkQueueDepthMeasurement) createSummary() (measurement.Summary, error) {
	perfData := &measurementutil.PerfData{
		Version:   kcmWorkQueueDepthMetricVersion,
		DataItems: []measurementutil.DataItem{},
	}
	for resource, value := range m.depthMetrics {
		perfData.DataItems = append(perfData.DataItems, measurementutil.DataItem{
			Data: map[string]float64{
				"Min": value,
			},
			Labels: map[string]string{
				"Resource": resource,
			},
		})
	}
	content, err := util.PrettyPrintJSON(perfData)
	if err != nil {
		return nil, err
	}
	if _, found := m.depthMetrics[hpaResource]; !found {
		return nil, fmt.Errorf("can't find work queue depth metric for %v", hpaResource)
	}
	// If the min value is consistently larger than the allowed the workqueue backlog,
	// it means the controller can't keep up with load for reconciliation.
	if m.hpaAllowedBacklog != nil && m.depthMetrics[hpaResource] > *m.hpaAllowedBacklog {
		err = errors.NewMetricViolationError(kcmWorkQueueDepthMetricName, fmt.Sprintf("HPA work queue backlog %v exceeds the allowed threshold %v", m.depthMetrics[hpaResource], *m.hpaAllowedBacklog))
		klog.Errorf("%s: %v", m, err)
	}
	return measurement.CreateSummary(kcmWorkQueueDepthMetricName, "json", content), err
}

func (e *kcmWorkQueueDepthMeasurement) getKCMWorkQueueDepth(host string, provider provider.Provider, port int) (map[string]float64, error) {
	cmd := fmt.Sprintf("curl -k https://localhost:%d/metrics", port)
	samples, err := e.sshKCMMetrics(cmd, host, provider)
	if err != nil {
		return nil, err
	}
	workQueueDepth := map[string]float64{}
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] != "workqueue_depth" {
			continue
		}
		resourceName, found := sample.Metric["name"]
		if !found {
			continue
		}
		workQueueDepth[string(resourceName)] = float64(sample.Value)
	}
	return workQueueDepth, nil
}

func (e *kcmWorkQueueDepthMeasurement) sshKCMMetrics(cmd, host string, provider provider.Provider) ([]*model.Sample, error) {
	sshResult, err := measurementutil.SSH(cmd, host+":22", provider)
	if err != nil {
		return nil, fmt.Errorf("unexpected error (code: %d) in ssh connection to master: %#v", sshResult.Code, err)
	} else if sshResult.Code != 0 {
		return nil, fmt.Errorf("failed running command: %s on the host: %s, result: %+v", cmd, host, sshResult)
	}
	data := sshResult.Stdout
	return measurementutil.ExtractMetricSamples(data)
}

func getMaxOrMin(existing, new map[string]float64, max bool) map[string]float64 {
	for resource, value := range new {
		if _, found := existing[resource]; !found {
			existing[resource] = value
		}
		existing[resource] = math.Min(existing[resource], value)
	}
	return existing
}
