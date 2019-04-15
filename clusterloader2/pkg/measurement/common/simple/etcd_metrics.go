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

package simple

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	etcdMetricsMetricName = "EtcdMetrics"
)

func init() {
	measurement.Register(etcdMetricsMetricName, createEtcdMetricsMeasurement)
}

func createEtcdMetricsMeasurement() measurement.Measurement {
	return &etcdMetricsMeasurement{
		stopCh:  make(chan struct{}),
		wg:      &sync.WaitGroup{},
		metrics: newEtcdMetrics(),
	}
}

type etcdMetricsMeasurement struct {
	isRunning bool
	stopCh    chan struct{}
	wg        *sync.WaitGroup
	metrics   *etcdMetrics
}

// Execute supports two actions:
// - start - Starts collecting etcd metrics.
// - gather - Gathers and prints etcd metrics summary.
func (e *etcdMetricsMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}
	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return summaries, err
	}
	host, err := util.GetStringOrDefault(config.Params, "host", config.ClusterFramework.GetClusterConfig().MasterIP)
	if err != nil {
		return summaries, err
	}

	switch action {
	case "start":
		klog.Infof("%s: starting etcd metrics collecting...", e)
		waitTime, err := util.GetDurationOrDefault(config.Params, "waitTime", time.Minute)
		if err != nil {
			return summaries, err
		}
		e.startCollecting(host, provider, waitTime)
		return summaries, nil
	case "gather":
		if err = e.stopAndSummarize(host, provider); err != nil {
			return summaries, err
		}
		content, err := util.PrettyPrintJSON(e.metrics)
		if err != nil {
			return summaries, err
		}
		summary := measurement.CreateSummary(etcdMetricsMetricName, "json", content)
		summaries := append(summaries, summary)
		return summaries, nil
	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (e *etcdMetricsMeasurement) Dispose() {
	if e.isRunning {
		e.isRunning = false
		close(e.stopCh)
		e.wg.Wait()
	}
}

func (e *etcdMetricsMeasurement) String() string {
	return etcdMetricsMetricName
}

func (e *etcdMetricsMeasurement) startCollecting(host, provider string, interval time.Duration) {
	e.isRunning = true
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			select {
			case <-time.After(interval):
				dbSize, err := e.getEtcdDatabaseSize(host, provider)
				if err != nil {
					klog.Errorf("%s: failed to collect etcd database size", e)
					continue
				}
				e.metrics.MaxDatabaseSize = math.Max(e.metrics.MaxDatabaseSize, dbSize)
			case <-e.stopCh:
				return
			}
		}
	}()
}

func (e *etcdMetricsMeasurement) stopAndSummarize(host, provider string) error {
	defer e.Dispose()
	// Do some one-off collection of metrics.
	samples, err := e.getEtcdMetrics(host, provider)
	if err != nil {
		return err
	}
	for _, sample := range samples {
		switch sample.Metric[model.MetricNameLabel] {
		case "etcd_disk_backend_commit_duration_seconds_bucket":
			measurementutil.ConvertSampleToBucket(sample, &e.metrics.BackendCommitDuration)
		case "etcd_debugging_snap_save_total_duration_seconds_bucket":
			measurementutil.ConvertSampleToBucket(sample, &e.metrics.SnapshotSaveTotalDuration)
		case "etcd_disk_wal_fsync_duration_seconds_bucket":
			measurementutil.ConvertSampleToBucket(sample, &e.metrics.WalFsyncDuration)
		case "etcd_network_peer_round_trip_time_seconds_bucket":
			measurementutil.ConvertSampleToBucket(sample, &e.metrics.PeerRoundTripTime)
		}
	}
	return nil
}

func (e *etcdMetricsMeasurement) getEtcdMetrics(host, provider string) ([]*model.Sample, error) {
	// Etcd is only exposed on localhost level. We are using ssh method
	if provider == "gke" {
		klog.Infof("%s: not grabbing etcd metrics through master SSH: unsupported for gke", e)
		return nil, nil
	}

	// In https://github.com/kubernetes/kubernetes/pull/74690, mTLS is enabled for etcd server
	// http://localhost:2382 is specified to bypass TLS credential requirement when checking
	// etcd /metrics and /health.
	if samples, err := e.sshEtcdMetrics("curl http://localhost:2382/metrics", host, provider); err == nil {
		return samples, nil
	}

	// Use old endpoint if new one fails.
	return e.sshEtcdMetrics("curl http://localhost:2379/metrics", host, provider)
}

func (e *etcdMetricsMeasurement) sshEtcdMetrics(cmd, host, provider string) ([]*model.Sample, error) {
	sshResult, err := measurementutil.SSH(cmd, host+":22", provider)
	if err != nil || sshResult.Code != 0 {
		return nil, fmt.Errorf("unexpected error (code: %d) in ssh connection to master: %#v", sshResult.Code, err)
	}
	data := sshResult.Stdout

	return measurementutil.ExtractMetricSamples(data)
}

func (e *etcdMetricsMeasurement) getEtcdDatabaseSize(host, provider string) (float64, error) {
	samples, err := e.getEtcdMetrics(host, provider)
	if err != nil {
		return 0, err
	}
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] == "etcd_debugging_mvcc_db_total_size_in_bytes" {
			return float64(sample.Value), nil
		}
	}
	return 0, fmt.Errorf("couldn't find etcd database size metric")
}

type etcdMetrics struct {
	BackendCommitDuration     measurementutil.HistogramVec `json:"backendCommitDuration"`
	SnapshotSaveTotalDuration measurementutil.HistogramVec `json:"snapshotSaveTotalDuration"`
	PeerRoundTripTime         measurementutil.HistogramVec `json:"peerRoundTripTime"`
	WalFsyncDuration          measurementutil.HistogramVec `json:"walFsyncDuration"`
	MaxDatabaseSize           float64                      `json:"maxDatabaseSize"`
}

func newEtcdMetrics() *etcdMetrics {
	return &etcdMetrics{
		BackendCommitDuration:     make(measurementutil.HistogramVec, 0),
		SnapshotSaveTotalDuration: make(measurementutil.HistogramVec, 0),
		PeerRoundTripTime:         make(measurementutil.HistogramVec, 0),
		WalFsyncDuration:          make(measurementutil.HistogramVec, 0),
	}
}
