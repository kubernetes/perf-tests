/*
Copyright 2020 The Kubernetes Authors.

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
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common/executors"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"

	_ "k8s.io/perf-tests/clusterloader2/pkg/flags" // init klog
)

var (
	// klogv1 allows users to turn on/off logging to stderr only through
	// the use of flag. This prevents us from having control over which
	// of the test functions have that mechanism turned off when we run
	// go test command.
	// TODO(#1286): refactor api_responsiveness_prometheus.go to make
	// testing of logging easier and remove this hack in the end.
	klogLogToStderr = true
)

func turnOffLoggingToStderrInKlog(t *testing.T) {
	if klogLogToStderr {
		err := flag.Set("logtostderr", "false")
		if err != nil {
			t.Errorf("Unable to set flag %v", err)
			return
		}
		err = flag.Set("v", "2")
		if err != nil {
			t.Errorf("Unable to set flag %v", err)
			return
		}
		flag.Parse()
		klogLogToStderr = false
	}
}

type sample struct {
	resource    string
	subresource string
	verb        string
	scope       string
	latency     float64
	count       int
	slowCount   int
}
type summaryEntry struct {
	resource    string
	subresource string
	verb        string
	scope       string
	p50         float64
	p90         float64
	p99         float64
	count       string
	slowCount   string
}

type fakeQueryExecutor struct {
	samples []*sample
}

func (ex *fakeQueryExecutor) Query(query string, queryTime time.Time) ([]*model.Sample, error) {
	samples := make([]*model.Sample, 0)
	for _, s := range ex.samples {
		sample := &model.Sample{
			Metric: model.Metric{
				"resource":    model.LabelValue(s.resource),
				"subresource": model.LabelValue(s.subresource),
				"verb":        model.LabelValue(s.verb),
				"scope":       model.LabelValue(s.scope),
			},
		}

		if strings.HasPrefix(query, "sum(increase") {
			if strings.Contains(query, "_count") {
				// countQuery
				sample.Value = model.SampleValue(s.count)
			} else {
				// countFastQuery
				// This is query is called 3 times, but to avoid complex fake
				// the same value is returned every time. The logic can handle
				// duplicates well, so this shouldn't be an issue.
				sample.Value = model.SampleValue(s.count - s.slowCount)
			}
		} else if strings.HasPrefix(query, "histogram_quantile") {
			// simpleLatencyQuery
			sample.Value = model.SampleValue(s.latency)
		} else if strings.HasPrefix(query, "quantile_over_time") {
			// latencyQuery
			sample.Metric["quantile"] = ".99"
			sample.Value = model.SampleValue(s.latency)
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func TestAPIResponsivenessSLOFailures(t *testing.T) {
	cases := []struct {
		name               string
		useSimple          bool
		allowedSlow        int
		hasError           bool
		testSeriesFile     string
		testSeriesDuration time.Duration
	}{
		{
			name:               "slo_pass",
			hasError:           false,
			testSeriesFile:     "slo_pass.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "below_slow_count_pass",
			hasError:           false,
			allowedSlow:        1,
			testSeriesFile:     "below_slow_count_pass.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "above_slow_count_failure",
			hasError:           true,
			allowedSlow:        1,
			testSeriesFile:     "above_slow_count_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "mutating_slo_failure",
			hasError:           true,
			testSeriesFile:     "mutating_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "get_slo_failure",
			hasError:           true,
			testSeriesFile:     "get_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "namespace_list_slo_failure",
			hasError:           true,
			testSeriesFile:     "namespace_list_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "cluster_list_slo_failure",
			hasError:           true,
			testSeriesFile:     "cluster_list_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "slo_pass_simple",
			useSimple:          true,
			hasError:           false,
			testSeriesFile:     "slo_pass.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "mutating_slo_failure_simple",
			useSimple:          true,
			hasError:           true,
			testSeriesFile:     "mutating_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "get_slo_failure_simple",
			useSimple:          true,
			hasError:           true,
			testSeriesFile:     "get_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "namespace_list_slo_failure_simple",
			useSimple:          true,
			hasError:           true,
			testSeriesFile:     "namespace_list_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
		{
			name:               "cluster_list_slo_failure_simple",
			useSimple:          true,
			hasError:           true,
			testSeriesFile:     "cluster_list_slo_failure.yaml",
			testSeriesDuration: 10 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor, err := executors.NewPromqlExecutor(fmt.Sprintf("../testdata/api_responsiveness_prometheus/%s", tc.testSeriesFile))
			if err != nil {
				t.Fatalf("failed to create PromQL executor: %v", err)
			}
			defer executor.Close()
			gatherer := &apiResponsivenessGatherer{}
			config := &measurement.Config{
				Params: map[string]interface{}{
					"useSimpleLatencyQuery": tc.useSimple,
					"allowedSlowCalls":      tc.allowedSlow,
				},
			}
			start := time.Unix(0, 0).UTC()
			end := start.Add(tc.testSeriesDuration)
			_, err = gatherer.Gather(executor, start, end, config)
			if tc.hasError {
				assert.NotNil(t, err, "wanted error, but got none")
			} else {
				assert.Nil(t, err, "wanted no error, but got %v", err)
			}
		})
	}
}

func TestAPIResponsivenessSummary(t *testing.T) {
	cases := []struct {
		name        string
		samples     []*sample
		summary     []*summaryEntry
		allowedSlow int
	}{
		{
			name:        "single_entry",
			allowedSlow: 0,
			samples: []*sample{
				{
					resource:  "pod",
					verb:      "POST",
					scope:     "resource",
					latency:   1.2,
					count:     123,
					slowCount: 5,
				},
			},
			summary: []*summaryEntry{
				{
					resource:  "pod",
					verb:      "POST",
					scope:     "resource",
					p99:       1200.,
					count:     "123",
					slowCount: "5",
				},
			},
		},
		{
			name:        "single_entry_with_slow_calls_enabled",
			allowedSlow: 1,
			samples: []*sample{
				{
					resource:  "pod",
					verb:      "POST",
					scope:     "resource",
					latency:   1.2,
					count:     123,
					slowCount: 5,
				},
			},
			summary: []*summaryEntry{
				{
					resource:  "pod",
					verb:      "POST",
					scope:     "resource",
					p99:       1200.,
					count:     "123",
					slowCount: "5",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := &fakeQueryExecutor{samples: tc.samples}
			gatherer := &apiResponsivenessGatherer{}
			config := &measurement.Config{
				Params: map[string]interface{}{
					"allowedSlowCalls": tc.allowedSlow,
				},
			}

			summaries, err := gatherer.Gather(executor, time.Now(), time.Now(), config)
			if !errors.IsMetricViolationError(err) {
				t.Fatal("unexpected error: ", err)
			}
			checkSummary(t, summaries, tc.summary)
		})
	}
}

func checkSummary(t *testing.T, got []measurement.Summary, wanted []*summaryEntry) {
	assert.Lenf(t, got, 1, "wanted single summary, got %d", len(got))
	var perfData measurementutil.PerfData
	if err := json.Unmarshal([]byte(got[0].SummaryContent()), &perfData); err != nil {
		t.Errorf("unable to unmarshal summary: %v", err)
		return
	}
	assert.Equal(t, currentAPICallMetricsVersion, perfData.Version)
	assert.Len(t, perfData.DataItems, len(wanted))

	toKey := func(resource, subresource, verb, scope string) string {
		return fmt.Sprintf("%s-%s-%s-%s", resource, subresource, verb, scope)
	}

	items := make(map[string]*measurementutil.DataItem)
	for _, item := range perfData.DataItems {
		items[toKey(
			item.Labels["Resource"],
			item.Labels["Subresource"],
			item.Labels["Verb"],
			item.Labels["Scope"])] = &item
	}

	for _, entry := range wanted {
		item, ok := items[toKey(entry.resource, entry.subresource, entry.verb, entry.scope)]
		if !ok {
			t.Errorf("%s in %s: %s %s wanted, but not found", entry.verb, entry.scope, entry.resource, entry.subresource)
			continue
		}
		assert.Equal(t, "ms", item.Unit)
		assert.Equal(t, entry.p50, item.Data["Perc50"])
		assert.Equal(t, entry.p90, item.Data["Perc90"])
		assert.Equal(t, entry.p99, item.Data["Perc99"])
		assert.Equal(t, entry.count, item.Labels["Count"])
		assert.Equal(t, entry.slowCount, item.Labels["SlowCount"])
	}
}

func TestLogging(t *testing.T) {
	cases := []struct {
		name               string
		samples            []*sample
		expectedMessages   []string
		unexpectedMessages []string
	}{
		{
			name: "print_5_warnings",
			samples: []*sample{
				{
					resource: "r1",
					verb:     "POST",
					scope:    "resource",
					latency:  1.2,
				},
				{
					resource: "r2",
					verb:     "POST",
					scope:    "resource",
					latency:  .9,
				},
				{
					resource: "r3",
					verb:     "POST",
					scope:    "resource",
					latency:  .8,
				},
				{
					resource: "r4",
					verb:     "POST",
					scope:    "resource",
					latency:  .7,
				},
				{
					resource: "r5",
					verb:     "POST",
					scope:    "resource",
					latency:  .6,
				},
				{
					resource: "r6",
					verb:     "POST",
					scope:    "resource",
					latency:  .5,
				},
			},
			expectedMessages: []string{
				": WARNING Top latency metric: {Resource:r1",
				": Top latency metric: {Resource:r2",
				": Top latency metric: {Resource:r3",
				": Top latency metric: {Resource:r4",
				": Top latency metric: {Resource:r5",
			},
			unexpectedMessages: []string{
				"Resource:r6",
			},
		},
		{
			name: "print_all_violations",
			samples: []*sample{
				{
					resource: "r1",
					verb:     "POST",
					scope:    "resource",
					latency:  1.2,
				},
				{
					resource: "r2",
					verb:     "POST",
					scope:    "resource",
					latency:  1.9,
				},
				{
					resource: "r3",
					verb:     "POST",
					scope:    "resource",
					latency:  1.8,
				},
				{
					resource: "r4",
					verb:     "POST",
					scope:    "resource",
					latency:  1.7,
				},
				{
					resource: "r5",
					verb:     "POST",
					scope:    "resource",
					latency:  1.6,
				},
				{
					resource: "r6",
					verb:     "POST",
					scope:    "resource",
					latency:  1.5,
				},
				{
					resource: "r7",
					verb:     "POST",
					scope:    "resource",
					latency:  .5,
				},
			},
			expectedMessages: []string{
				": WARNING Top latency metric: {Resource:r1",
				": WARNING Top latency metric: {Resource:r2",
				": WARNING Top latency metric: {Resource:r3",
				": WARNING Top latency metric: {Resource:r4",
				": WARNING Top latency metric: {Resource:r5",
				": WARNING Top latency metric: {Resource:r6",
			},
			unexpectedMessages: []string{
				"Resource:r7",
			},
		},
	}

	turnOffLoggingToStderrInKlog(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			klog.SetOutput(buf)

			executor := &fakeQueryExecutor{samples: tc.samples}
			gatherer := &apiResponsivenessGatherer{}
			config := &measurement.Config{}

			_, err := gatherer.Gather(executor, time.Now(), time.Now(), config)
			if err != nil && !errors.IsMetricViolationError(err) {
				t.Errorf("error while gathering results: %v", err)
			}
			klog.Flush()

			for _, msg := range tc.expectedMessages {
				assert.Contains(t, buf.String(), msg)
			}
			for _, msg := range tc.unexpectedMessages {
				assert.NotContains(t, buf.String(), msg)
			}
		})
	}
}

func TestAPIResponsivenessCustomThresholds(t *testing.T) {
	splitter := func(yamlLines []string) string {
		return strings.Join(yamlLines, "\n")
	}

	cases := []struct {
		name             string
		config           *measurement.Config
		samples          []*sample
		hasError         bool
		expectedMessages []string
	}{
		{
			name: "simple_slo_threshold_override_success",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customThresholds": splitter([]string{
						"- verb: PUT",
						"  resource: leases",
						"  scope: namespace",
						"  threshold: 600ms",
					}),
				},
			},
			samples: []*sample{
				{
					resource: "leases",
					verb:     "PUT",
					scope:    "namespace",
					latency:  0.5,
				},
			},
			hasError: false,
		},
		{
			name: "simple_slo_threshold_override_failure",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customThresholds": splitter([]string{
						"- verb: PUT",
						"  resource: leases",
						"  scope: namespace",
						"  threshold: 400ms",
					}),
				},
			},
			samples: []*sample{
				{
					resource: "leases",
					verb:     "PUT",
					scope:    "namespace",
					latency:  0.5,
				},
			},
			hasError: true,
			expectedMessages: []string{
				"WARNING Top latency metric",
			},
		},
		{
			name: "empty_custom_thresholds_field",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customThresholds": "",
				},
			},
			samples: []*sample{
				{
					resource: "leases",
					verb:     "PUT",
					scope:    "namespace",
					latency:  0.5,
				},
			},
			hasError: false,
		},
		{
			name: "no_custom_thresholds_field",
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
			samples: []*sample{
				{
					resource: "leases",
					verb:     "PUT",
					scope:    "namespace",
					latency:  0.5,
				},
			},
			hasError: false,
		},
		{
			name: "unrecognized_metric",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customThresholds": splitter([]string{
						"- verb: POST",
						"  resource: pod",
						"  scope: namespace",
						"  threshold: 500ms",
					}),
				},
			},
			samples: []*sample{
				{
					resource: "leases",
					verb:     "PUT",
					scope:    "namespace",
					latency:  0.2,
				},
			},
			hasError: false,
			expectedMessages: []string{
				"unrecognized custom threshold API call key",
			},
		},
		{
			name: "non_unmarshallable_custom_thresholds",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customThresholds": splitter([]string{
						"im: not",
						"a: good",
						"yaml: array",
					}),
				},
			},
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  0.2,
				},
			},
			hasError: true,
		},
	}

	turnOffLoggingToStderrInKlog(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			klog.SetOutput(buf)

			executor := &fakeQueryExecutor{samples: tc.samples}
			gatherer := &apiResponsivenessGatherer{}

			_, err := gatherer.Gather(executor, time.Now(), time.Now(), tc.config)
			klog.Flush()
			if tc.hasError {
				assert.NotNil(t, err, "expected an error, but got none")
			} else {
				assert.Nil(t, err, "expected no error, but got %v", err)
			}

			for _, msg := range tc.expectedMessages {
				assert.Contains(t, buf.String(), msg)
			}
		})
	}
}
