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

	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	apiResponsivenessPrometheusMeasurementName = "APIResponsivenessPrometheus"

	// Thresholds for API call latency as defined in the official K8s SLO
	// https://github.com/kubernetes/community/blob/master/sig-scalability/slos/api_call_latency.md
	resourceThreshold  time.Duration = 1 * time.Second
	namespaceThreshold time.Duration = 5 * time.Second
	clusterThreshold   time.Duration = 30 * time.Second

	currentAPICallMetricsVersion = "v1"

	// TODO(krzysied): figure out why we're getting non-capitalized proxy and fix this
	filters = `resource!="events", verb!~"WATCH|WATCHLIST|PROXY|proxy|CONNECT"`

	// latencyQuery matches description of the API call latency SLI and measure 99th percentaile over 5m windows
	//
	// latencyQuery: %v should be replaced with (1) filters and (2) query window size..
	latencyQuery = "quantile_over_time(0.99, apiserver:apiserver_request_latency_1m:histogram_quantile{%v}[%v])"

	// simpleLatencyQuery measures 99th percentile of API call latency  over given period of time
	// it doesn't match SLI, but is useful in shorter tests, where we don't have enough number of windows to use latencyQuery meaningfully.
	//
	// simpleLatencyQuery: placeholders should be replaced with (1) quantile (2) filters and (3) query window size.
	simpleLatencyQuery = "histogram_quantile(%.2f, sum(rate(apiserver_request_duration_seconds_bucket{%v}[%v])) by (resource,  subresource, verb, scope, le))"

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

type apiCall struct {
	Resource    string                        `json:"resource"`
	Subresource string                        `json:"subresource"`
	Verb        string                        `json:"verb"`
	Scope       string                        `json:"scope"`
	Latency     measurementutil.LatencyMetric `json:"latency"`
	Count       int                           `json:"count"`
}

type apiResponsiveness struct {
	APICalls []apiCall `json:"apicalls"`
}

func (a *apiResponsiveness) Len() int { return len(a.APICalls) }
func (a *apiResponsiveness) Swap(i, j int) {
	a.APICalls[i], a.APICalls[j] = a.APICalls[j], a.APICalls[i]
}
func (a *apiResponsiveness) Less(i, j int) bool {
	return a.APICalls[i].Latency.Perc99 < a.APICalls[j].Latency.Perc99
}

type apiResponsivenessGatherer struct{}

func (a *apiResponsivenessGatherer) Gather(executor QueryExecutor, startTime time.Time, config *measurement.MeasurementConfig) (measurement.Summary, error) {
	apiCalls, err := a.gatherAPICalls(executor, startTime, config)
	if err != nil {
		klog.Errorf("%s: samples gathering error: %v", config.Identifier, err)
		return nil, err
	}

	metrics := &apiResponsiveness{APICalls: apiCalls}

	badMetrics := validateAPICalls(config.Identifier, metrics)

	content, err := util.PrettyPrintJSON(apiCallToPerfData(metrics))
	if err != nil {
		return nil, err
	}

	summaryName, err := util.GetStringOrDefault(config.Params, "summaryName", apiResponsivenessPrometheusMeasurementName)
	if err != nil {
		return nil, err
	}

	summary := measurement.CreateSummary(summaryName, "json", content)
	if len(badMetrics) > 0 {
		return summary, errors.NewMetricViolationError("top latency metric", fmt.Sprintf("there should be no high-latency requests, but: %v", badMetrics))
	}
	return summary, nil
}

func (a *apiResponsivenessGatherer) String() string {
	return apiResponsivenessPrometheusMeasurementName
}

func (a *apiResponsivenessGatherer) IsEnabled(config *measurement.MeasurementConfig) bool {
	return true
}

func (a *apiResponsivenessGatherer) gatherAPICalls(executor QueryExecutor, startTime time.Time, config *measurement.MeasurementConfig) ([]apiCall, error) {
	measurementEnd := time.Now()
	measurementDuration := measurementEnd.Sub(startTime)

	useSimple, err := util.GetBoolOrDefault(config.Params, "useSimpleLatencyQuery", false)
	if err != nil {
		return nil, err
	}

	var latencySamples []*model.Sample
	if useSimple {
		promDuration := measurementutil.ToPrometheusTime(measurementDuration)
		quantiles := []float64{0.5, 0.9, 0.99}
		for _, q := range quantiles {
			query := fmt.Sprintf(simpleLatencyQuery, q, filters, promDuration)
			samples, err := executor.Query(query, measurementEnd)
			if err != nil {
				return nil, err
			}
			// Underlying code assumes presence of 'quantile' label, so adding it manually.
			for _, sample := range samples {
				sample.Metric["quantile"] = model.LabelValue(fmt.Sprintf("%.2f", q))
			}
			latencySamples = append(latencySamples, samples...)
		}
	} else {
		// Latency measurement is based on 5m window aggregation,
		// therefore first 5 minutes of the test should be skipped.
		latencyMeasurementDuration := measurementDuration - latencyWindowSize
		if latencyMeasurementDuration < time.Minute {
			latencyMeasurementDuration = time.Minute
		}
		promDuration := measurementutil.ToPrometheusTime(latencyMeasurementDuration)

		query := fmt.Sprintf(latencyQuery, filters, promDuration)
		latencySamples, err = executor.Query(query, measurementEnd)
		if err != nil {
			return nil, err
		}
	}

	timeBoundedCountQuery := fmt.Sprintf(countQuery, filters, measurementutil.ToPrometheusTime(measurementDuration))
	countSamples, err := executor.Query(timeBoundedCountQuery, measurementEnd)
	if err != nil {
		return nil, err
	}
	return a.convertToAPICalls(latencySamples, countSamples)
}

func (a *apiResponsivenessGatherer) convertToAPICalls(latencySamples, countSamples []*model.Sample) ([]apiCall, error) {
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

func getAPICall(apiCalls map[string]*apiCall, resource, subresource, verb, scope string) *apiCall {
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
	call := getAPICall(apiCalls, resource, subresource, verb, scope)
	call.Latency.SetQuantile(quantile, latency)
}

func addCount(apiCalls map[string]*apiCall, resource, subresource, verb, scope string, count int) {
	if count == 0 {
		return
	}
	call := getAPICall(apiCalls, resource, subresource, verb, scope)
	call.Count = count
}

func getMetricKey(resource, subresource, verb, scope string) string {
	return fmt.Sprintf("%s|%s|%s|%s", resource, subresource, verb, scope)
}

func getSLOThreshold(verb, scope string) time.Duration {
	if verb != "LIST" {
		return resourceThreshold
	}
	if scope == "cluster" {
		return clusterThreshold
	}
	return namespaceThreshold
}

func validateAPICalls(identifier string, metrics *apiResponsiveness) []string {
	badMetrics := make([]string, 0)
	top := topToPrint

	sort.Sort(sort.Reverse(metrics))
	for _, apiCall := range metrics.APICalls {
		isBad := false
		sloThreshold := getSLOThreshold(apiCall.Verb, apiCall.Scope)
		if err := apiCall.Latency.VerifyThreshold(sloThreshold); err != nil {
			isBad = true
			badMetrics = append(badMetrics, fmt.Sprintf("got: %+v; expected perc99 <= %v", apiCall, sloThreshold))
		}
		if top > 0 || isBad {
			top--
			prefix := ""
			if isBad {
				prefix = "WARNING "
			}
			klog.Infof("%s: %vTop latency metric: %+v; threshold: %v", identifier, prefix, apiCall, sloThreshold)
		}
	}
	return badMetrics
}

// apiCallToPerfData transforms apiResponsiveness to PerfData.
func apiCallToPerfData(apicalls *apiResponsiveness) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentAPICallMetricsVersion}
	for _, apicall := range apicalls.APICalls {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Perc50": float64(apicall.Latency.Perc50) / 1000000, // us -> ms
				"Perc90": float64(apicall.Latency.Perc90) / 1000000,
				"Perc99": float64(apicall.Latency.Perc99) / 1000000,
			},
			Unit: "ms",
			Labels: map[string]string{
				"Verb":        apicall.Verb,
				"Resource":    apicall.Resource,
				"Subresource": apicall.Subresource,
				"Scope":       apicall.Scope,
				"Count":       fmt.Sprintf("%v", apicall.Count),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}
