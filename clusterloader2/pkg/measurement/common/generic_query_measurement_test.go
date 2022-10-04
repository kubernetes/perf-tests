/*
Copyright 2022 The Kubernetes Authors.

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
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

type fakeQueryExecutor struct {
	samples map[string][]*model.Sample
}

func (f fakeQueryExecutor) Query(query string, _ time.Time) ([]*model.Sample, error) {
	return f.samples[query], nil
}

func TestGather(t *testing.T) {
	testCases := []struct {
		desc             string
		params           map[string]interface{}
		samples          map[string][]*model.Sample
		wantDataItems    []measurementutil.DataItem
		wantConfigureErr string
		wantErr          string
	}{
		{
			desc: "happy path",
			params: map[string]interface{}{
				"metricName":    "happy-path",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":      "no-samples",
						"query":     "no-samples-query[%v]",
						"threshold": 42,
					},
					{
						"name":      "below-threshold",
						"query":     "below-threshold-query[%v]",
						"threshold": 30,
					},
					{
						"name":  "no-threshold",
						"query": "no-threshold-query[%v]",
					},
					{
						"name":  "multiple-duration-placeholders",
						"query": "placeholder-a[%v] + placeholder-b[%v]",
					},
				},
			},
			samples: map[string][]*model.Sample{
				"below-threshold-query[1m]":             {{Value: model.SampleValue(7)}},
				"no-threshold-query[1m]":                {{Value: model.SampleValue(120)}},
				"placeholder-a[1m] + placeholder-b[1m]": {{Value: model.SampleValue(5)}},
			},
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "ms",
					Data: map[string]float64{
						"below-threshold":                7.0,
						"no-threshold":                   120.0,
						"multiple-duration-placeholders": 5.0,
					},
				},
			},
		},
		{
			desc: "no samples, but samples not required",
			params: map[string]interface{}{
				"metricName":    "no-samples",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":  "no-samples",
						"query": "no-samples-query[%v]",
					},
				},
			},
		},
		{
			desc: "no samples, but samples required",
			params: map[string]interface{}{
				"metricName":    "no-samples",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":           "no-samples",
						"query":          "no-samples-query[%v]",
						"requireSamples": true,
					},
				},
			},
			wantErr: "no samples",
		},
		{
			desc: "too many samples",
			params: map[string]interface{}{
				"metricName":    "many-samples",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":  "many-samples",
						"query": "many-samples-query[%v]",
					},
				},
			},
			samples: map[string][]*model.Sample{
				"many-samples-query[1m]": {
					{Value: model.SampleValue(1)},
					{Value: model.SampleValue(2)},
				},
			},
			wantErr: "too many samples",
			// When too many samples, first value is returned and error is raised.
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "ms",
					Data: map[string]float64{
						"many-samples": 1.0,
					},
				},
			},
		},
		{
			desc: "sample above threshold",
			params: map[string]interface{}{
				"metricName":    "above-threshold",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":      "above-threshold",
						"query":     "above-threshold-query[%v]",
						"threshold": 60,
					},
				},
			},
			samples: map[string][]*model.Sample{
				"above-threshold-query[1m]": {{Value: model.SampleValue(123)}},
			},
			wantErr: "sample above threshold: want: less or equal than 60, got: 123",
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "ms",
					Data: map[string]float64{
						"above-threshold": 123.0,
					},
				},
			},
		},
		{
			desc: "missing field metricName",
			params: map[string]interface{}{
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":      "no-samples",
						"query":     "no-samples-query[%v]",
						"threshold": 42,
					},
					{
						"name":      "below-threshold",
						"query":     "below-threshold-query[%v]",
						"threshold": 30,
					},
					{
						"name":  "no-threshold",
						"query": "no-threshold-query[%v]",
					},
				},
			},
			wantConfigureErr: "metricName is required",
		},
		{
			desc: "dimensions",
			params: map[string]interface{}{
				"metricName":    "dimensions",
				"metricVersion": "v1",
				"unit":          "ms",
				"dimensions": []interface{}{
					"d1",
					"d2",
				},
				"queries": []map[string]interface{}{
					{
						"name":  "perc99",
						"query": "query-perc99[%v]",
					},
					{
						"name":  "perc90",
						"query": "query-perc90[%v]",
					},
				},
			},
			samples: map[string][]*model.Sample{
				"query-perc99[1m]": {
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val1"),
							model.LabelName("d3"): model.LabelValue("d3-val1"), // Ignored
						},
						Value: model.SampleValue(1),
					},
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val2"),
						},
						Value: model.SampleValue(2),
					},
				},
				"query-perc90[1m]": {
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val1"),
						},
						Value: model.SampleValue(3),
					},
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val2"),
						},
						Value: model.SampleValue(4),
					},
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							// d2 not set
						},
						Value: model.SampleValue(5),
					},
				},
			},
			wantDataItems: []measurementutil.DataItem{
				{
					Labels: map[string]string{
						"d1": "d1-val1",
						"d2": "d2-val1",
					},
					Unit: "ms",
					Data: map[string]float64{
						"perc99": 1.0,
						"perc90": 3.0,
					},
				},
				{
					Labels: map[string]string{
						"d1": "d1-val1",
						"d2": "d2-val2",
					},
					Unit: "ms",
					Data: map[string]float64{
						"perc99": 2.0,
						"perc90": 4.0,
					},
				},
				{
					Labels: map[string]string{
						"d1": "d1-val1",
						"d2": "",
					},
					Unit: "ms",
					Data: map[string]float64{
						// perc99 doesn't return this combination.
						"perc90": 5.0,
					},
				},
			},
		},
		{
			desc: "multiple values for single dimension",
			params: map[string]interface{}{
				"metricName":    "dimensions",
				"metricVersion": "v1",
				"unit":          "ms",
				"dimensions": []interface{}{
					"d1",
					"d2",
				},
				"queries": []map[string]interface{}{
					{
						"name":  "perc99",
						"query": "query-perc99[%v]",
					},
				},
			},
			samples: map[string][]*model.Sample{
				"query-perc99[1m]": {
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val1"),
						},
						Value: model.SampleValue(1),
					},
					{
						Metric: model.Metric{
							model.LabelName("d1"): model.LabelValue("d1-val1"),
							model.LabelName("d2"): model.LabelValue("d2-val1"),
						},
						Value: model.SampleValue(2),
					},
				},
			},
			wantErr: "too many samples for [d1-val1 d2-val1]",
			wantDataItems: []measurementutil.DataItem{
				{
					Labels: map[string]string{
						"d1": "d1-val1",
						"d2": "d2-val1",
					},
					Unit: "ms",
					Data: map[string]float64{
						"perc99": 1.0,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gatherer := &genericQueryGatherer{}
			err := gatherer.Configure(&measurement.Config{Params: tc.params})
			if tc.wantConfigureErr != "" {
				assert.Contains(t, err.Error(), tc.wantConfigureErr)
				return
			}
			assert.Nil(t, err)
			startTime := time.Now()
			endTime := startTime.Add(1 * time.Minute)
			executor := fakeQueryExecutor{tc.samples}

			summaries, err := gatherer.Gather(executor, startTime, endTime, nil)
			if tc.wantErr != "" {
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				assert.Nil(t, err)
			}
			require.Len(t, summaries, 1)
			content := summaries[0].SummaryContent()
			perfData := measurementutil.PerfData{}
			err = json.Unmarshal([]byte(content), &perfData)
			require.Nil(t, err)
			assert.ElementsMatch(t, perfData.DataItems, tc.wantDataItems)
		})
	}
}
