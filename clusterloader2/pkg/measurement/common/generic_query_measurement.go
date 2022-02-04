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

	"github.com/prometheus/common/model"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	name = "GenericPrometheusQuery"
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&genericQueryGatherer{})
	}
	if err := measurement.Register(name, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", name, err)
	}
}

type genericQueryGatherer struct {
	metricName    string
	metricVersion string
	queries       []genericQuery
	unit          string
}

type genericQuery struct {
	name         string
	query        string
	hasThreshold bool
	threshold    float64
}

func (g *genericQueryGatherer) Configure(config *measurement.Config) error {
	metricName, err := util.GetString(config.Params, "metricName")
	if err != nil {
		return err
	}

	metricVersion, err := util.GetString(config.Params, "metricVersion")
	if err != nil {
		return err
	}

	unit, err := util.GetString(config.Params, "unit")
	if err != nil {
		return err
	}

	queries, err := util.GetMapArray(config.Params, "queries")
	if err != nil {
		return err
	}
	var genericQueries []genericQuery
	for _, q := range queries {
		query, err := util.GetString(q, "query")
		if err != nil {
			return err
		}
		name, err := util.GetString(q, "name")
		if err != nil {
			return err
		}
		hasThreshold := true
		threshold, err := util.GetFloat64(q, "threshold")
		if util.IsErrKeyNotFound(err) {
			klog.V(2).Infof("No threshold set for %v: %v", metricName, name)
			hasThreshold = false
		} else if err != nil {
			return err
		}
		genericQueries = append(genericQueries, genericQuery{name, query, hasThreshold, threshold})
	}

	g.metricName = metricName
	g.metricVersion = metricVersion
	g.unit = unit
	g.queries = genericQueries
	return nil
}

func (g *genericQueryGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (g *genericQueryGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	var errs []error
	data := map[string]float64{}
	for _, q := range g.queries {
		samples, err := g.query(q, executor, startTime, endTime)
		if err != nil {
			return nil, err
		}

		if len(samples) > 1 {
			errs = append(errs, errors.NewMetricViolationError(q.name, fmt.Sprintf("too many samples: query returned %v streams, expected 1", len(samples))))
		}
		if len(samples) == 0 {
			klog.Warningf("query returned no samples for %v: %v", g.metricName, q.name)
			continue
		}

		val := float64(samples[0].Value)

		thresholdMsg := "none"
		if q.hasThreshold {
			thresholdMsg = fmt.Sprintf("%v", q.threshold)
		}
		klog.V(2).Infof("metric: %v: %v, value: %v, threshold: %v", g.metricName, q.name, val, thresholdMsg)

		if q.hasThreshold && val > q.threshold {
			errs = append(errs, errors.NewMetricViolationError(q.name, fmt.Sprintf("sample above threshold: want: less or equal than %v, got: %v", q.threshold, val)))
		}

		data[q.name] = val
	}
	summary, err := g.createSummary(g.metricName, data)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		err = errors.NewMetricViolationError(g.metricName, fmt.Sprintf("%v", errs))
	}
	return []measurement.Summary{summary}, err
}

func (g *genericQueryGatherer) String() string {
	return name
}

func (g *genericQueryGatherer) query(q genericQuery, executor QueryExecutor, startTime, endTime time.Time) ([]*model.Sample, error) {
	duration := endTime.Sub(startTime)
	boundedQuery := fmt.Sprintf(q.query, measurementutil.ToPrometheusTime(duration))
	klog.V(2).Infof("bounded query: %s, duration: %v", boundedQuery, duration)
	return executor.Query(boundedQuery, endTime)
}

func (g *genericQueryGatherer) createSummary(name string, data map[string]float64) (measurement.Summary, error) {
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version: g.metricVersion,
		DataItems: []measurementutil.DataItem{
			{
				Data: data,
				Unit: g.unit,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(name, "json", content), nil
}
