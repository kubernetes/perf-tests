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

/*
TODO(krzysied): This measurement should replace api_responsiveness.go.
*/

package slos

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	apiResponsivenessPrometheusMeasurementName = "APIResponsivenessPrometheus"

	filters = `resource!="events", verb!~"WATCH|WATCHLIST|PROXY|proxy|CONNECT"`

	// latencyQuery: %v should be replaced with (1) filters and (2) query window size.
	// TODO(krzysied): figure out why we're getting non-capitalized proxy and fix this.
	latencyQuery = "quantile_over_time(0.99, apiserver:apiserver_request_latency:histogram_quantile{%v}[%v])"

	// countQuery %v should be replaced with (1) filters and (2) query window size.
	countQuery = "sum(increase(apiserver_request_duration_seconds_count{%v}[%v])) by (resource, subresource, scope, verb)"

	latencyWindowSize = 5 * time.Minute

	// Number of metrics with highest latency to print. If the latency exceeeds SLO threshold, a metric is printed regardless.
	topToPrint = 5
)

func init() {
	create := func() measurement.Measurement { return createPrometheusMeasurement(&apiResponsivenessGatherer{}) }
	if err := measurement.Register(apiResponsivenessPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", apiResponsivenessPrometheusMeasurementName, err)
	}
}

type apiResponsivenessGatherer struct{}

func (a *apiResponsivenessGatherer) Gather(executor QueryExecutor, startTime time.Time) (measurement.Summary, error) {
	apiCalls, err := a.gatherApiCalls(executor, startTime)
	if err != nil {
		klog.Errorf("%s: samples gathering error: %v", apiResponsivenessMeasurementName, err)
		return nil, err
	}

	metrics := &apiResponsiveness{ApiCalls: apiCalls}
	sort.Sort(sort.Reverse(metrics))
	var badMetrics []string
	top := topToPrint
	for _, apiCall := range metrics.ApiCalls {
		isBad := false
		sloThreshold := getSLOThreshold(apiCall.Verb, apiCall.Scope)
		if apiCall.Latency.Perc99 > sloThreshold {
			isBad = true
			badMetrics = append(badMetrics, fmt.Sprintf("got: %+v; expected perc99 <= %v", apiCall, sloThreshold))
		}
		if top > 0 || isBad {
			top--
			prefix := ""
			if isBad {
				prefix = "WARNING "
			}
			klog.Infof("%s: %vTop latency metric: %+v; threshold: %v", apiResponsivenessMeasurementName, prefix, apiCall, sloThreshold)
		}
	}

	content, err := util.PrettyPrintJSON(apiCallToPerfData(metrics))
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(apiResponsivenessPrometheusMeasurementName, "json", content)
	// TODO(#498): For testing purpose this metric will never return metric violation error.
	// The code below should be
	// if len(badMetrics) > 0 {
	// 	return summary, errors.NewMetricViolationError("top latency metric", fmt.Sprintf("there should be no high-latency requests, but: %v", badMetrics))
	// }
	return summary, nil
}

func (a *apiResponsivenessGatherer) String() string {
	return apiResponsivenessPrometheusMeasurementName
}

func (a *apiResponsivenessGatherer) gatherApiCalls(executor QueryExecutor, startTime time.Time) ([]apiCall, error) {
	measurementEnd := time.Now()
	measurementDuration := measurementEnd.Sub(startTime)
	// Latency measurement is based on 5m window aggregation,
	// therefore first 5 minutes of the test should be skipped.
	latencyMeasurementDuration := measurementDuration - latencyWindowSize
	if latencyMeasurementDuration < time.Minute {
		latencyMeasurementDuration = time.Minute
	}
	timeBoundedLatencyQuery := fmt.Sprintf(latencyQuery, filters, measurementutil.ToPrometheusTime(latencyMeasurementDuration))
	latencySamples, err := executor.Query(timeBoundedLatencyQuery, measurementEnd)
	if err != nil {
		return nil, err
	}
	timeBoundedCountQuery := fmt.Sprintf(countQuery, filters, measurementutil.ToPrometheusTime(measurementDuration))
	countSamples, err := executor.Query(timeBoundedCountQuery, measurementEnd)
	if err != nil {
		return nil, err
	}
	return a.convertToApiCalls(latencySamples, countSamples)
}

func (a *apiResponsivenessGatherer) convertToApiCalls(latencySamples, countSamples []*model.Sample) ([]apiCall, error) {
	apiCalls := make(map[string]*apiCall)

	for _, sample := range latencySamples {
		resource := string(sample.Metric["resource"])
		subresource := string(sample.Metric["subresource"])
		verb := string(sample.Metric["verb"])
		scope := string(sample.Metric["scope"])
		quantile, err := strconv.ParseFloat(string(sample.Metric["quantile"]), 64)
		if err != nil {
			return nil, err
		}

		latency := time.Duration(float64(sample.Value) * float64(time.Second))
		addLatency(apiCalls, resource, subresource, verb, scope, quantile, latency)
	}

	for _, sample := range countSamples {
		resource := string(sample.Metric["resource"])
		subresource := string(sample.Metric["subresource"])
		verb := string(sample.Metric["verb"])
		scope := string(sample.Metric["scope"])

		count := int(math.Round(float64(sample.Value)))
		addCount(apiCalls, resource, subresource, verb, scope, count)
	}

	var result []apiCall
	for _, call := range apiCalls {
		result = append(result, *call)
	}
	return result, nil
}

func getApiCall(apiCalls map[string]*apiCall, resource, subresource, verb, scope string) *apiCall {
	key := getMetricKey(resource, subresource, verb, scope)
	call, exists := apiCalls[key]
	if !exists {
		call = &apiCall{
			Resource:    resource,
			Subresource: subresource,
			Verb:        verb,
			Scope:       scope,
		}
		apiCalls[key] = call
	}
	return call
}

func addLatency(apiCalls map[string]*apiCall, resource, subresource, verb, scope string, quantile float64, latency time.Duration) {
	call := getApiCall(apiCalls, resource, subresource, verb, scope)
	call.Latency.SetQuantile(quantile, latency)
}

func addCount(apiCalls map[string]*apiCall, resource, subresource, verb, scope string, count int) {
	if count == 0 {
		return
	}
	call := getApiCall(apiCalls, resource, subresource, verb, scope)
	call.Count = count
}

func getMetricKey(resource, subresource, verb, scope string) string {
	return fmt.Sprintf("%s|%s|%s|%s", resource, subresource, verb, scope)
}

func getSLOThreshold(verb, scope string) time.Duration {
	if verb != "LIST" {
		return apiCallLatencyThreshold
	}
	if scope == "cluster" {
		return apiClusterScopeListCallThreshold
	}
	return apiListCallLatencyThreshold
}
