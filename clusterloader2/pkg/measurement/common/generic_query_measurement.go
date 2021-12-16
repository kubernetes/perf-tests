/*
Copyright 2021 The Kubernetes Authors.

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

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	name = "GenericPrometheusQuery"
)

func init() {
	create := func() measurement.Measurement { return CreatePrometheusMeasurement(&genericQueryGatherer{}) }
	if err := measurement.Register(name, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", name, err)
	}
}

type genericQueryGatherer struct {
	metricName    string
	metricVersion string
	rawQuery      string
}

func (g *genericQueryGatherer) Configure(config *measurement.Config) error {
	query, err := util.GetString(config.Params, "rawQuery")
	if err != nil {
		return err
	}
	metricName, err := util.GetString(config.Params, "metricName")
	if err != nil {
		return err
	}
	metricVersion, err := util.GetString(config.Params, "metricVersion")
	if err != nil {
		return err
	}

	g.rawQuery = query
	g.metricName = metricName
	g.metricVersion = metricVersion
	return nil
}

func (g *genericQueryGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (g *genericQueryGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	metric, err := g.query(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}

	klog.V(2).Infof("%s: got %v", g.metricName, metric)
	summary, err := g.createSummary(metric)
	return []measurement.Summary{summary}, err
}

func (g *genericQueryGatherer) String() string {
	return name
}

func (g *genericQueryGatherer) query(executor QueryExecutor, startTime, endTime time.Time) (*measurementutil.LatencyMetric, error) {
	duration := endTime.Sub(startTime)
	boundedQuery := fmt.Sprintf(g.rawQuery, measurementutil.ToPrometheusTime(duration))
	klog.V(2).Infof("bounded query: %s, duration: %v", boundedQuery, duration)
	samples, err := executor.Query(boundedQuery, endTime)
	if err != nil {
		return nil, err
	}
	// For now, only latency is supported.
	return measurementutil.NewLatencyMetricPrometheus(samples)
}

func (g *genericQueryGatherer) createSummary(latency *measurementutil.LatencyMetric) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version:   g.metricVersion,
		DataItems: []measurementutil.DataItem{latency.ToPerfData(g.metricName)},
	})
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(g.metricName, "json", content), nil
}
