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
	goerrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	genericPrometheusQueryMeasurementName = "GenericPrometheusQuery"
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&genericQueryGatherer{})
	}
	if err := measurement.Register(genericPrometheusQueryMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", genericPrometheusQueryMeasurementName, err)
	}
}

type genericQueryGatherer struct {
	StartParams
}

// StartParams represents configuration that can be passed as params
// with action: start.
type StartParams struct {
	MetricName    string
	MetricVersion string
	Queries       []GenericQuery
	Unit          string
	Dimensions    []string
}

// TODO(mborsz): github.com/go-playground/validator or similar project?
func (p *StartParams) Validate() error {
	if p.MetricName == "" {
		return goerrors.New("metricName is required")
	}
	if p.MetricVersion == "" {
		return goerrors.New("metricVersion is required")
	}
	if p.Unit == "" {
		return goerrors.New("unit is required")
	}

	for idx, query := range p.Queries {
		if err := query.Validate(); err != nil {
			return fmt.Errorf("params.queries[%d] validation failed: %v", idx, err)
		}
	}

	return nil
}

type GenericQuery struct {
	Name           string
	Query          string
	Threshold      *float64
	RequireSamples bool
}

func (q *GenericQuery) Validate() error {
	if q.Name == "" {
		return goerrors.New("name is required")
	}
	if q.Query == "" {
		return goerrors.New("query is required")
	}
	return nil
}

func (g *genericQueryGatherer) Configure(config *measurement.Config) error {
	if err := util.ToStruct(config.Params, &g.StartParams); err != nil {
		return err
	}
	return g.StartParams.Validate()
}

func (g *genericQueryGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func key(metric model.Metric, dimensions []string) (string, map[string]string) {
	s := make([]string, 0)
	m := make(map[string]string)
	for _, dimension := range dimensions {
		val := string(metric[model.LabelName(dimension)])
		s = append(s, val)
		m[dimension] = val
	}
	return fmt.Sprintf("%v", s), m
}

func getOrCreate(dataItems map[string]*measurementutil.DataItem, key, unit string, labels map[string]string) *measurementutil.DataItem {
	dataItem, ok := dataItems[key]
	if ok {
		return dataItem
	}
	dataItem = &measurementutil.DataItem{
		Data:   make(map[string]float64),
		Unit:   unit,
		Labels: labels,
	}
	dataItems[key] = dataItem
	return dataItem
}

func (g *genericQueryGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	var errs []error
	dataItems := map[string]*measurementutil.DataItem{}
	for _, q := range g.Queries {
		samples, err := g.query(q, executor, startTime, endTime)
		if err != nil {
			return nil, err
		}

		if len(samples) == 0 {
			if q.RequireSamples {
				errs = append(errs, errors.NewMetricViolationError(q.Name, fmt.Sprintf("query returned no samples for %v", g.MetricName)))
			}
			klog.Warningf("query returned no samples for %v: %v", g.MetricName, q.Name)
			continue
		}

		for _, sample := range samples {
			k, labels := key(sample.Metric, g.Dimensions)
			dataItem := getOrCreate(dataItems, k, g.Unit, labels)

			val := float64(sample.Value)
			prevVal, ok := dataItem.Data[q.Name]
			if ok {
				errs = append(errs, errors.NewMetricViolationError(q.Name, fmt.Sprintf("too many samples for %s: query returned %v and %v, expected single value.", k, val, prevVal)))
			} else {
				dataItem.Data[q.Name] = val
			}

			thresholdMsg := "none"
			if q.Threshold != nil {
				thresholdMsg = fmt.Sprintf("%v", *q.Threshold)
			}
			klog.V(2).Infof("metric: %v: %v, value: %v, threshold: %v", g.MetricName, q.Name, val, thresholdMsg)
			if q.Threshold != nil && val > *q.Threshold {
				errs = append(errs, errors.NewMetricViolationError(q.Name, fmt.Sprintf("sample above threshold: want: less or equal than %v, got: %v", *q.Threshold, val)))
			}
		}
	}
	summary, err := g.createSummary(g.MetricName, dataItems)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		err = errors.NewMetricViolationError(g.MetricName, fmt.Sprintf("%v", errs))
	}
	return []measurement.Summary{summary}, err
}

func (g *genericQueryGatherer) String() string {
	return genericPrometheusQueryMeasurementName
}

func (g *genericQueryGatherer) query(q GenericQuery, executor QueryExecutor, startTime, endTime time.Time) ([]*model.Sample, error) {
	duration := endTime.Sub(startTime)
	// Replace all provided duration placeholders (%v) with the test duration.
	boundedQuery := strings.ReplaceAll(q.Query, "%v", measurementutil.ToPrometheusTime(duration))
	klog.V(2).Infof("bounded query: %s, duration: %v", boundedQuery, duration)
	return executor.Query(boundedQuery, endTime)
}

func (g *genericQueryGatherer) createSummary(metricName string, dataItems map[string]*measurementutil.DataItem) (measurement.Summary, error) {
	perfData := &measurementutil.PerfData{
		Version:   g.MetricVersion,
		DataItems: nil,
	}

	for _, dataItem := range dataItems {
		perfData.DataItems = append(perfData.DataItems, *dataItem)
	}

	content, err := util.PrettyPrintJSON(perfData)
	if err != nil {
		return nil, err
	}
	// Replace '_' by spaces as '_' is used as delimiter to extract metricName from file name
	return measurement.CreateSummary(genericPrometheusQueryMeasurementName+" "+metricName, "json", content), nil
}
