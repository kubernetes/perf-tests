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

package common

import (
	"fmt"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func CreatePrometheusMeasurement(gatherer Gatherer) measurement.Measurement {
	return &prometheusMeasurement{
		gatherer: gatherer,
	}
}

// QueryExecutor is an interface for queryning Prometheus server.
type QueryExecutor interface {
	Query(query string, queryTime time.Time) ([]*model.Sample, error)
}

// Gatherer is an interface for measurements based on Prometheus metrics. Those measurments don't require any preparation.
// It's assumed Prometheus is up, running and instructed to scrape required metrics in the test cluster
// (please see clusterloader2/pkg/prometheus/manifests).
type Gatherer interface {
	Configure(config *measurement.Config) error
	Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error)
	IsEnabled(config *measurement.Config) bool
	String() string
}

type prometheusMeasurement struct {
	gatherer Gatherer

	startTime time.Time
}

func (m *prometheusMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	prometheusClient, err := util.GetStringOrDefault(config.Params, "prometheusClient", "inCluster")
	if err != nil {
		return nil, err
	}
	if prometheusClient != "inCluster" && prometheusClient != "managed" {
		return nil, fmt.Errorf("unknown Prometheus client")
	}
	if prometheusClient == "inCluster" && config.PrometheusFramework == nil {
		klog.Warningf("%s: Prometheus is disabled, skipping the measurement!", config.Identifier)
		return nil, nil
	}

	if !m.gatherer.IsEnabled(config) {
		klog.Warningf("%s: disabled, skipping the measuerment!", config.Identifier)
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if err := m.gatherer.Configure(config); err != nil {
			return nil, err
		}
		klog.V(2).Infof("%s has started", config.Identifier)
		m.startTime = time.Now()
		return nil, nil
	case "gather":
		klog.V(2).Infof("%s gathering results", config.Identifier)
		enableViolations, err := util.GetBoolOrDefault(config.Params, "enableViolations", false)
		if err != nil {
			return nil, err
		}

		var pc prom.Client
		switch prometheusClient {
		case "inCluster":
			pc = prom.NewInClusterPrometheusClient(config.PrometheusFramework.GetClientSets().GetClient())
		case "managed":
			pc, err = config.CloudProvider.GetManagedPrometheusClient()
			if err != nil {
				return nil, fmt.Errorf("error while creating managed Prometheus client: %w", err)
			}
		}
		executor := measurementutil.NewQueryExecutor(pc)

		summary, err := m.gatherer.Gather(executor, m.startTime, time.Now(), config)
		if err != nil {
			if !errors.IsMetricViolationError(err) {
				klog.Errorf("%s gathering error: %v", config.Identifier, err)
				return nil, err
			}
			if !enableViolations {
				err = nil
			}
		}
		return summary, err
	default:
		return nil, fmt.Errorf("unknown action: %v", action)
	}
}

func (m *prometheusMeasurement) Dispose() {}

func (m *prometheusMeasurement) String() string {
	return m.gatherer.String()
}
