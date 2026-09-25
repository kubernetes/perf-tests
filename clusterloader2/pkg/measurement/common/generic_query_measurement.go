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
	"math"
	"slices"
	"sort"
	"strconv"
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
	defaultSubqueryStep                   = 30 * time.Second
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

	for idx, q := range p.Queries {
		if err := q.Validate(len(p.Queries) > 1); err != nil {
			return fmt.Errorf("params.queries[%d] validation failed: %v", idx, err)
		}
	}

	return nil
}

type GenericQuery struct {
	Name           string
	Query          string
	Aggregations   []string
	Threshold      *float64
	LowerBound     bool
	RequireSamples bool
}

func (q *GenericQuery) Validate(multipleQueries bool) error {
	if q.Query == "" {
		return goerrors.New("query is required")
	}
	if len(q.Aggregations) == 0 {
		if q.Name == "" {
			return goerrors.New("name is required")
		}
		return nil
	}
	if multipleQueries && q.Name == "" {
		return goerrors.New("name is required when multiple queries use aggregations")
	}
	for _, agg := range q.Aggregations {
		if err := validateAggregation(agg); err != nil {
			return err
		}
	}
	return nil
}

func validateAggregation(agg string) error {
	lower := strings.ToLower(agg)
	switch lower {
	case "max", "min", "avg", "sum", "count", "last":
		return nil
	}
	if strings.HasPrefix(lower, "perc") {
		p, err := strconv.ParseFloat(agg[4:], 64)
		if err != nil || p < 0 || p > 100 {
			return fmt.Errorf("invalid percentile aggregation %q: must be between 0 and 100", agg)
		}
		return nil
	}
	return fmt.Errorf("unsupported aggregation %q", agg)
}

func computeAggregation(agg string, values []model.SamplePair) (float64, error) {
	if len(values) == 0 {
		return 0, goerrors.New("no sample values")
	}
	floats := make([]float64, len(values))
	var sum float64
	for i, v := range values {
		fv := float64(v.Value)
		floats[i] = fv
		sum += fv
	}

	lower := strings.ToLower(agg)
	switch lower {
	case "max":
		return slices.Max(floats), nil
	case "min":
		return slices.Min(floats), nil
	case "avg":
		return sum / float64(len(floats)), nil
	case "sum":
		return sum, nil
	case "count":
		return float64(len(floats)), nil
	case "last":
		return floats[len(floats)-1], nil
	}
	if strings.HasPrefix(lower, "perc") {
		p, err := strconv.ParseFloat(agg[4:], 64)
		if err != nil || p < 0 || p > 100 {
			return 0, fmt.Errorf("invalid percentile aggregation %q", agg)
		}
		return computePrometheusQuantile(p/100.0, floats), nil
	}
	return 0, fmt.Errorf("unsupported aggregation %q", agg)
}

// computePrometheusQuantile matches Prometheus's quantile_over_time linear interpolation.
func computePrometheusQuantile(q float64, values []float64) float64 {
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 1 {
		return sorted[0]
	}
	rank := q * float64(n-1)
	lowerIndex := max(0, int(math.Floor(rank)))
	upperIndex := min(n-1, lowerIndex+1)
	weight := rank - math.Floor(rank)
	return sorted[lowerIndex]*(1-weight) + sorted[upperIndex]*weight
}

func metricDataKey(queryName, agg string) string {
	if queryName == "" {
		return agg
	}
	return fmt.Sprintf("%s_%s", queryName, agg)
}

func stripPromQLComments(query string) string {
	lines := strings.Split(query, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func (g *genericQueryGatherer) buildSubquery(rawQuery string, duration time.Duration) string {
	promDuration := measurementutil.ToPrometheusTime(duration)
	promStep := measurementutil.ToPrometheusTime(defaultSubqueryStep)
	cleaned := strings.ReplaceAll(stripPromQLComments(rawQuery), "%v", promDuration)
	return fmt.Sprintf("(%s)[%s:%s]", cleaned, promDuration, promStep)
}

func (g *genericQueryGatherer) Configure(config *measurement.Config) error {
	if err := util.ToStruct(config.Params, &g.StartParams); err != nil {
		return err
	}
	return g.StartParams.Validate()
}

func (g *genericQueryGatherer) IsEnabled(_ *measurement.Config) bool {
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

func (g *genericQueryGatherer) validateSample(q GenericQuery, val float64) error {
	thresholdMsg := "none"
	if q.Threshold != nil {
		thresholdMsg = fmt.Sprintf("%v (upper bound)", *q.Threshold)
		if q.LowerBound {
			thresholdMsg = fmt.Sprintf("%v (lower bound)", *q.Threshold)
		}
	}
	klog.V(2).Infof("metric: %v: %v, value: %v, threshold: %v", g.MetricName, q.Name, val, thresholdMsg)
	if q.Threshold != nil {
		if q.LowerBound && val < *q.Threshold {
			return errors.NewMetricViolationError(q.Name, fmt.Sprintf("sample below threshold: want: greater or equal than %v, got: %v", *q.Threshold, val))
		}
		if !q.LowerBound && val > *q.Threshold {
			return errors.NewMetricViolationError(q.Name, fmt.Sprintf("sample above threshold: want: less or equal than %v, got: %v", *q.Threshold, val))
		}
	}
	return nil
}

func (g *genericQueryGatherer) gatherAggregatedQuery(q GenericQuery, executor QueryExecutor, startTime, endTime time.Time, dataItems map[string]*measurementutil.DataItem, errs *[]error) error {
	matrixExec, ok := executor.(MatrixQueryExecutor)
	if !ok {
		return fmt.Errorf("executor %T does not support matrix queries required for aggregations", executor)
	}

	duration := endTime.Sub(startTime)
	subquery := g.buildSubquery(q.Query, duration)
	klog.V(2).Infof("subquery: %s, duration: %v", subquery, duration)

	matrix, err := matrixExec.QueryMatrix(subquery, endTime)
	if err != nil {
		return err
	}
	if len(matrix) == 0 {
		qLabel := q.Name
		if qLabel == "" {
			qLabel = strings.Join(q.Aggregations, ",")
		}
		if q.RequireSamples {
			*errs = append(*errs, errors.NewMetricViolationError(qLabel, fmt.Sprintf("query returned no samples for %v", g.MetricName)))
		}
		klog.Warningf("query returned no samples for %v: %v", g.MetricName, qLabel)
		return nil
	}

	for _, stream := range matrix {
		k, labels := key(stream.Metric, g.Dimensions)
		dataItem := getOrCreate(dataItems, k, g.Unit, labels)
		for _, agg := range q.Aggregations {
			val, err := computeAggregation(agg, stream.Values)
			if err != nil {
				return err
			}
			dataKey := metricDataKey(q.Name, agg)
			if prevVal, exists := dataItem.Data[dataKey]; exists {
				*errs = append(*errs, errors.NewMetricViolationError(dataKey, fmt.Sprintf("too many samples for %s: query returned %v and %v, expected single value.", k, val, prevVal)))
			} else {
				dataItem.Data[dataKey] = val
			}
		}
	}
	return nil
}

func (g *genericQueryGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, _ *measurement.Config) ([]measurement.Summary, error) {
	var errs []error
	dataItems := map[string]*measurementutil.DataItem{}
	for _, q := range g.Queries {
		if len(q.Aggregations) > 0 {
			if err := g.gatherAggregatedQuery(q, executor, startTime, endTime, dataItems, &errs); err != nil {
				return nil, err
			}
			continue
		}

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

			if err := g.validateSample(q, val); err != nil {
				errs = append(errs, err)
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
