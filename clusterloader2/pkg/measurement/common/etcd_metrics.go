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
	"fmt"
	"math"
	"os"
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
	if err := measurement.Register(etcdMetricsMetricName, createEtcdMetricsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", etcdMetricsMetricName, err)
	}
}

func createEtcdMetricsMeasurement() measurement.Measurement {
	return &etcdMetricsMeasurement{
		stopCh:  make(chan struct{}),
		wg:      &sync.WaitGroup{},
		metrics: newEtcdMetrics(),
	}
}

type etcdMetricsMeasurement struct {
	sync.Mutex
	isRunning bool
	stopCh    chan struct{}
	wg        *sync.WaitGroup
	metrics   *etcdMetrics
}

// Execute supports two actions:
// - start - Starts collecting etcd metrics.
// - gather - Gathers and prints etcd metrics summary.
func (e *etcdMetricsMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	// Etcd is only exposed on localhost level. We are using ssh method
	if !config.ClusterFramework.GetClusterConfig().IsSSHToMasterSupported {
		klog.Infof("not grabbing etcd metrics through master SSH: unsupported for provider, %s", config.ClusterFramework.GetClusterConfig().Provider)
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return nil, err
	}
	hosts := config.ClusterFramework.GetClusterConfig().MasterIPs
	if len(hosts) < 1 {
		klog.Warningf("ETCD measurements will be disabled due to no MasterIps: %v", hosts)
		return nil, nil
	}

	etcdInsecurePort := config.ClusterFramework.GetClusterConfig().EtcdInsecurePort
	switch action {
	case "start":
		klog.Infof("%s: starting etcd metrics collecting...", e)
		waitTime, err := util.GetDurationOrDefault(config.Params, "waitTime", time.Minute)
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			e.startCollecting(h, provider, waitTime, etcdInsecurePort)
		}
		return nil, nil
	case "gather":
		for _, h := range hosts {
			if err = e.stopAndSummarize(h, provider, etcdInsecurePort); err != nil {
				return nil, err
			}
		}
		content, err := util.PrettyPrintJSON(e.metrics)
		if err != nil {
			return nil, err
		}
		summary := measurement.CreateSummary(etcdMetricsMetricName, "json", content)
		return []measurement.Summary{summary}, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
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

func (e *etcdMetricsMeasurement) startCollecting(host, provider string, interval time.Duration, port int) {
	e.isRunning = true
	e.wg.Add(1)

	collectEtcdDatabaseSize := func() error {
		dbSize, err := e.getEtcdDatabaseSize(host, provider, port)
		if err != nil {
			return err
		}

		e.Lock()
		defer e.Unlock()
		e.metrics.MaxDatabaseSize = math.Max(e.metrics.MaxDatabaseSize, dbSize)

		return nil
	}
	go func() {
		defer e.wg.Done()
		for {
			select {
			case <-time.After(interval):
				err := collectEtcdDatabaseSize()
				if err != nil {
					klog.Errorf("%s: failed to collect etcd database size", e)
					continue
				}
			case <-e.stopCh:
				return
			}
		}
	}()
}

func (e *etcdMetricsMeasurement) stopAndSummarize(host, provider string, port int) error {
	defer e.Dispose()
	// Do some one-off collection of metrics.
	samples, err := e.getEtcdMetrics(host, provider, port)
	if err != nil {
		return err
	}

	collectEtcdMetrics := func(sample *model.Sample) {
		var hist *measurementutil.HistogramVec
		switch sample.Metric[model.MetricNameLabel] {
		case "etcd_disk_backend_commit_duration_seconds_bucket":
			hist = &e.metrics.BackendCommitDuration
		case "etcd_debugging_snap_save_total_duration_seconds_bucket":
			hist = &e.metrics.SnapshotSaveTotalDuration
		case "etcd_disk_wal_fsync_duration_seconds_bucket":
			hist = &e.metrics.WalFsyncDuration
		case "etcd_network_peer_round_trip_time_seconds_bucket":
			hist = &e.metrics.PeerRoundTripTime
		default:
			return
		}

		e.Lock()
		measurementutil.ConvertSampleToBucket(sample, hist)
		e.Unlock()
	}
	for _, sample := range samples {
		collectEtcdMetrics(sample)
	}
	return nil
}

func (e *etcdMetricsMeasurement) getEtcdMetrics(host, provider string, port int) ([]*model.Sample, error) {

	// In https://github.com/kubernetes/kubernetes/pull/74690, mTLS is enabled for etcd server
	// in order to bypass TLS credential requirement when checking etc /metrics and /health, you
	// need to provide the insecure http port number to access etcd, http://localhost:2382 for
	// example.
	cmd := fmt.Sprintf("curl http://localhost:%d/metrics", port)
	if samples, err := e.sshEtcdMetrics(cmd, host, provider); err == nil {
		return samples, nil
	}

	// Use old endpoint if new one fails, "2379" is hard-coded here as well, it is kept as is since
	// we don't want to bloat the cluster config only for a fall-back attempt.
	etcdCert, etcdKey := os.Getenv("ETCD_CERTIFICATE"), os.Getenv("ETCD_KEY")
	if etcdCert == "" || etcdKey == "" {
		klog.Warning("empty etcd cert or key, using http")
		cmd = "curl http://localhost:2379/metrics"
	} else {
		cmd = fmt.Sprintf("curl -k --cert %s --key %s https://localhost:2379/metrics", etcdCert, etcdKey)
	}

	return e.sshEtcdMetrics(cmd, host, provider)
}

func (e *etcdMetricsMeasurement) sshEtcdMetrics(cmd, host, provider string) ([]*model.Sample, error) {
	sshResult, err := measurementutil.SSH(cmd, host+":22", provider)
	if err != nil {
		return nil, fmt.Errorf("unexpected error (code: %d) in ssh connection to master: %#v", sshResult.Code, err)
	} else if sshResult.Code != 0 {
		return nil, fmt.Errorf("failed running command: %s on the host: %s", cmd, host)
	}
	data := sshResult.Stdout

	return measurementutil.ExtractMetricSamples(data)
}

func (e *etcdMetricsMeasurement) getEtcdDatabaseSize(host, provider string, port int) (float64, error) {
	samples, err := e.getEtcdMetrics(host, provider, port)
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
