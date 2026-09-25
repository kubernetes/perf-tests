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
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	etcdMetricsMetricName       = "EtcdMetrics"
	etcdEventsMetricsMetricName = "EtcdEventsMetrics"
)

func init() {
	if err := measurement.Register(etcdMetricsMetricName, createEtcdMetricsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", etcdMetricsMetricName, err)
	}
}

func createEtcdMetricsMeasurement() measurement.Measurement {
	return &etcdMetricsMeasurement{
		stopCh:        make(chan struct{}),
		wg:            &sync.WaitGroup{},
		metrics:       newEtcdMetrics(),
		eventsMetrics: newEtcdMetrics(),
	}
}

type etcdMetricsMeasurement struct {
	sync.Mutex
	isRunning     bool
	stopCh        chan struct{}
	wg            *sync.WaitGroup
	metrics       *etcdMetrics
	eventsMetrics *etcdMetrics
}

// etcdInstance is a single etcd instance scraped over master SSH.
type etcdInstance struct {
	summaryName string
	// port is the insecure http port serving /metrics (etcd --listen-metrics-urls).
	port    int
	metrics *etcdMetrics
	// isMain marks the etcd instance backing everything but events. It is the only
	// one allowed to fall back to the hardcoded 2379 port, and the only one whose
	// scrape failure fails the measurement.
	isMain bool
}

// Execute supports two actions:
// - start - Starts collecting etcd metrics.
// - gather - Gathers and prints etcd metrics summary.
func (e *etcdMetricsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	provider := config.ClusterFramework.GetClusterConfig().Provider
	// Etcd is only exposed on localhost level. We are using ssh method
	if !provider.Features().SupportSSHToMaster {
		klog.Warningf("not grabbing etcd metrics through master SSH: unsupported for provider, %s", config.ClusterFramework.GetClusterConfig().Provider.Name())
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	hosts := config.ClusterFramework.GetClusterConfig().MasterIPs
	if len(hosts) < 1 {
		klog.Warningf("ETCD measurements will be disabled due to no MasterIps: %v", hosts)
		return nil, nil
	}

	instances := []*etcdInstance{{
		summaryName: etcdMetricsMetricName,
		port:        config.ClusterFramework.GetClusterConfig().EtcdInsecurePort,
		metrics:     e.metrics,
		isMain:      true,
	}}
	if port := config.ClusterFramework.GetClusterConfig().EtcdEventsInsecurePort; port > 0 {
		instances = append(instances, &etcdInstance{
			summaryName: etcdEventsMetricsMetricName,
			port:        port,
			metrics:     e.eventsMetrics,
		})
	}

	switch action {
	case "start":
		klog.V(2).Infof("%s: starting etcd metrics collecting...", e)
		waitTime, err := util.GetDurationOrDefault(config.Params, "waitTime", time.Minute)
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			for _, instance := range instances {
				e.startCollecting(h, provider, waitTime, instance)
			}
		}
		return nil, nil
	case "gather":
		var summaries []measurement.Summary
		for _, instance := range instances {
			collected := false
			for _, h := range hosts {
				if err := e.stopAndSummarize(h, provider, instance); err != nil {
					// Only the main etcd is guaranteed to expose its metrics port,
					// so a failure elsewhere must not fail the measurement.
					if instance.isMain {
						return nil, err
					}
					klog.Warningf("%s: failed to collect %s on %s: %v", e, instance.summaryName, h, err)
					continue
				}
				collected = true
			}
			if !collected {
				continue
			}
			content, err := util.PrettyPrintJSON(instance.metrics)
			if err != nil {
				return nil, err
			}
			summaries = append(summaries, measurement.CreateSummary(instance.summaryName, "json", content))
		}
		return summaries, nil
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

func (e *etcdMetricsMeasurement) startCollecting(host string, provider provider.Provider, interval time.Duration, instance *etcdInstance) {
	e.isRunning = true
	e.wg.Add(1)

	collectEtcdDatabaseSize := func() error {
		dbSize, err := e.getEtcdDatabaseSize(host, provider, instance)
		if err != nil {
			return err
		}

		e.Lock()
		defer e.Unlock()
		instance.metrics.MaxDatabaseSize = math.Max(instance.metrics.MaxDatabaseSize, dbSize)

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

func (e *etcdMetricsMeasurement) stopAndSummarize(host string, provider provider.Provider, instance *etcdInstance) error {
	defer e.Dispose()
	// Do some one-off collection of metrics.
	samples, err := e.getEtcdMetrics(host, provider, instance)
	if err != nil {
		return err
	}

	collectEtcdMetrics := func(sample *model.Sample) {
		var hist *measurementutil.HistogramVec
		switch sample.Metric[model.MetricNameLabel] {
		case "etcd_disk_backend_commit_duration_seconds_bucket":
			hist = &instance.metrics.BackendCommitDuration
		case "etcd_debugging_snap_save_total_duration_seconds_bucket":
			hist = &instance.metrics.SnapshotSaveTotalDuration
		case "etcd_disk_wal_fsync_duration_seconds_bucket":
			hist = &instance.metrics.WalFsyncDuration
		case "etcd_network_peer_round_trip_time_seconds_bucket":
			hist = &instance.metrics.PeerRoundTripTime
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

func (e *etcdMetricsMeasurement) getEtcdMetrics(host string, provider provider.Provider, instance *etcdInstance) ([]*model.Sample, error) {

	// In https://github.com/kubernetes/kubernetes/pull/74690, mTLS is enabled for etcd server
	// in order to bypass TLS credential requirement when checking etc /metrics and /health, you
	// need to provide the insecure http port number to access etcd, http://localhost:2382 for
	// example.
	cmd := fmt.Sprintf("curl http://localhost:%d/metrics", instance.port)
	samples, err := e.sshEtcdMetrics(cmd, host, provider)
	if err == nil {
		return samples, nil
	}
	if !instance.isMain {
		// 2379 is served by the main etcd, so falling back to it would report
		// main etcd's metrics under another instance's name.
		return nil, err
	}
	klog.Warningf("%s: call on %d port (%s) failed due to %v. Falling back to default 2379 port.", e, instance.port, cmd, err)

	// Use old endpoint if new one fails, "2379" is hard-coded here as well, it is kept as is since
	// we don't want to bloat the cluster config only for a fall-back attempt.
	etcdCert, etcdKey, etcdHost := os.Getenv("ETCD_CERTIFICATE"), os.Getenv("ETCD_KEY"), os.Getenv("ETCD_HOST")
	if etcdHost == "" {
		etcdHost = "localhost"
	}
	if etcdCert == "" || etcdKey == "" {
		klog.Warning("empty etcd cert or key, using http")
		cmd = fmt.Sprintf("curl http://%s:2379/metrics", etcdHost)
	} else {
		cmd = fmt.Sprintf("curl -k --cert %s --key %s https://%s:2379/metrics", etcdCert, etcdKey, etcdHost)
	}

	return e.sshEtcdMetrics(cmd, host, provider)
}

func (e *etcdMetricsMeasurement) sshEtcdMetrics(cmd, host string, provider provider.Provider) ([]*model.Sample, error) {
	sshResult, err := measurementutil.SSH(cmd, host+":22", provider)
	if err != nil {
		return nil, fmt.Errorf("unexpected error (code: %d) in ssh connection to master: %#v", sshResult.Code, err)
	} else if sshResult.Code != 0 {
		return nil, fmt.Errorf("failed running command: %s on the host: %s, result: %+v", cmd, host, sshResult)
	}
	data := sshResult.Stdout

	return measurementutil.ExtractMetricSamples(data)
}

func (e *etcdMetricsMeasurement) getEtcdDatabaseSize(host string, provider provider.Provider, instance *etcdInstance) (float64, error) {
	samples, err := e.getEtcdMetrics(host, provider, instance)
	if err != nil {
		return 0, err
	}
	for _, sample := range samples {
		if sample.Metric[model.MetricNameLabel] == "etcd_debugging_mvcc_db_total_size_in_bytes" ||
			sample.Metric[model.MetricNameLabel] == "etcd_mvcc_db_total_size_in_bytes" {
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
