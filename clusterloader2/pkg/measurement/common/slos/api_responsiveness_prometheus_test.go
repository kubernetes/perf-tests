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
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

type sample struct {
	resource    string
	subresource string
	verb        string
	scope       string
	latency     float64
	count       int
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
				"subresoruce": model.LabelValue(s.subresource),
				"verb":        model.LabelValue(s.verb),
				"scope":       model.LabelValue(s.scope),
			},
		}

		if strings.HasPrefix(query, "sum") {
			// countQuery
			sample.Value = model.SampleValue(s.count)
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
		name      string
		samples   []*sample
		useSimple bool
		hasError  bool
	}{
		{
			name:     "slo_pass",
			hasError: false,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  0.2,
				},
				{
					resource: "pod",
					verb:     "GET",
					scope:    "namespace",
					latency:  0.2,
				},
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "namespace",
					latency:  1.2,
				},
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "cluster",
					latency:  5.2,
				},
			},
		},
		{
			name:     "mutating_slo_failure",
			hasError: true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  1.2,
				},
			},
		},
		{
			name:     "get_slo_failure",
			hasError: true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "GET",
					scope:    "namespace",
					latency:  1.2,
				},
			},
		},
		{
			name:     "namespace_list_slo_failure",
			hasError: true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "namespace",
					latency:  5.2,
				},
			},
		},
		{
			name:     "cluster_list_slo_failure",
			hasError: true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "cluster",
					latency:  30.2,
				},
			},
		},
		{
			name:      "slo_pass_simple",
			useSimple: true,
			hasError:  false,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  0.2,
				},
				{
					resource: "pod",
					verb:     "GET",
					scope:    "namespace",
					latency:  0.2,
				},
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "namespace",
					latency:  1.2,
				},
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "cluster",
					latency:  5.2,
				},
			},
		},
		{
			name:      "mutating_slo_failure_simple",
			useSimple: true,
			hasError:  true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  1.2,
				},
			},
		},
		{
			name:      "get_slo_failure_simple",
			useSimple: true,
			hasError:  true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "GET",
					scope:    "namespace",
					latency:  1.2,
				},
			},
		},
		{
			name:      "namespace_list_slo_failure_simple",
			useSimple: true,
			hasError:  true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "namespace",
					latency:  5.2,
				},
			},
		},
		{
			name:      "cluster_list_slo_failure_simple",
			useSimple: true,
			hasError:  true,
			samples: []*sample{
				{
					resource: "pod",
					verb:     "LIST",
					scope:    "cluster",
					latency:  30.2,
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
					"useSimpleLatencyQuery": tc.useSimple,
				},
			}

			_, err := gatherer.Gather(executor, time.Now(), config)
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
		name    string
		samples []*sample
		summary []*summaryEntry
	}{
		{
			name: "single_entry",
			samples: []*sample{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					latency:  1.2,
					count:    123,
				},
			},
			summary: []*summaryEntry{
				{
					resource: "pod",
					verb:     "POST",
					scope:    "namespace",
					p99:      1200.,
					count:    "123",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := &fakeQueryExecutor{samples: tc.samples}
			gatherer := &apiResponsivenessGatherer{}
			config := &measurement.Config{}

			summaries, _ := gatherer.Gather(executor, time.Now(), config)
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
					latency:  1.2,
				},
				{
					resource: "r2",
					verb:     "POST",
					latency:  .9,
				},
				{
					resource: "r3",
					verb:     "POST",
					latency:  .8,
				},
				{
					resource: "r4",
					verb:     "POST",
					latency:  .7,
				},
				{
					resource: "r5",
					verb:     "POST",
					latency:  .6,
				},
				{
					resource: "r6",
					verb:     "POST",
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
					latency:  1.2,
				},
				{
					resource: "r2",
					verb:     "POST",
					latency:  1.9,
				},
				{
					resource: "r3",
					verb:     "POST",
					latency:  1.8,
				},
				{
					resource: "r4",
					verb:     "POST",
					latency:  1.7,
				},
				{
					resource: "r5",
					verb:     "POST",
					latency:  1.6,
				},
				{
					resource: "r6",
					verb:     "POST",
					latency:  1.5,
				},
				{
					resource: "r7",
					verb:     "POST",
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

	klog.InitFlags(nil)
	flag.Set("logtostderr", "false")
	flag.Parse()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			klog.SetOutput(buf)

			executor := &fakeQueryExecutor{samples: tc.samples}
			gatherer := &apiResponsivenessGatherer{}
			config := &measurement.Config{}

			gatherer.Gather(executor, time.Now(), config)
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
