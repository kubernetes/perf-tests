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

package slos

import (
	"fmt"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

const (
	dnsProgLat = "DNSProgrammingLatency"

	dnsProgQuery = "quantile_over_time(0.99, dns:coredns_programming_duration:histogram_quantile{}[%v])"
)

func init() {
	create := func() measurement.Measurement { return createPrometheusMeasurement(&dnsProgGatherer{}) }
	if err := measurement.Register(dnsProgLat, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", dnsProgLat, err)
	}
}

type dnsProgGatherer struct{}

func (d *dnsProgGatherer) IsEnabled(config *measurement.MeasurementConfig) bool {
	return true
}

func (d *dnsProgGatherer) String() string {
	return dnsProgLat
}

func (d *dnsProgGatherer) Gather(executor QueryExecutor, startTime time.Time, config *measurement.MeasurementConfig) (measurement.Summary, error) {
	latency, err := d.query(executor, startTime)
	if err != nil {
		return nil, err
	}

	klog.Infof("%s: got %v", dnsProgLat, latency)
	return createLatencySummary(latency, dnsProgLat)
}

func (d *dnsProgGatherer) query(executor QueryExecutor, startTime time.Time) (*measurementutil.LatencyMetric, error) {
	end := time.Now()
	duration := end.Sub(startTime)

	boundedQuery := fmt.Sprintf(dnsProgQuery, measurementutil.ToPrometheusTime(duration))

	samples, err := executor.Query(boundedQuery, end)
	if err != nil {
		return nil, err
	}
	if len(samples) != 3 {
		return nil, fmt.Errorf("got unexpected number of samples: %d", len(samples))
	}
	return measurementutil.NewLatencyMetricPrometheus(samples)
}
