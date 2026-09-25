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
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common/executors"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"sigs.k8s.io/yaml"
)

type fakeQueryExecutor struct {
	samples  map[string][]*model.Sample
	matrices map[string]model.Matrix
}

func (f fakeQueryExecutor) Query(query string, _ time.Time) ([]*model.Sample, error) {
	return f.samples[query], nil
}

func (f fakeQueryExecutor) QueryMatrix(query string, _ time.Time) (model.Matrix, error) {
	return f.matrices[query], nil
}

func TestGather(t *testing.T) {
	testCases := []struct {
		desc             string
		params           map[string]interface{}
		samples          map[string][]*model.Sample
		matrices         map[string]model.Matrix
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
				"below-threshold-query[60s]":              {{Value: model.SampleValue(7)}},
				"no-threshold-query[60s]":                 {{Value: model.SampleValue(120)}},
				"placeholder-a[60s] + placeholder-b[60s]": {{Value: model.SampleValue(5)}},
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
				"many-samples-query[60s]": {
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
				"above-threshold-query[60s]": {{Value: model.SampleValue(123)}},
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
			desc: "sample above lower bound",
			params: map[string]interface{}{
				"metricName":    "below-threshold",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":       "above-threshold",
						"query":      "above-threshold-query[%v]",
						"threshold":  60,
						"lowerBound": true,
					},
				},
			},
			samples: map[string][]*model.Sample{
				"above-threshold-query[60s]": {{Value: model.SampleValue(74)}},
			},
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "ms",
					Data: map[string]float64{
						"above-threshold": 74.0,
					},
				},
			},
		},
		{
			desc: "sample below lower bound",
			params: map[string]interface{}{
				"metricName":    "below-threshold",
				"metricVersion": "v1",
				"unit":          "ms",
				"queries": []map[string]interface{}{
					{
						"name":       "below-threshold",
						"query":      "below-threshold-query[%v]",
						"threshold":  60,
						"lowerBound": true,
					},
				},
			},
			samples: map[string][]*model.Sample{
				"below-threshold-query[60s]": {{Value: model.SampleValue(42)}},
			},
			wantErr: "sample below threshold: want: greater or equal than 60, got: 42",
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "ms",
					Data: map[string]float64{
						"below-threshold": 42.0,
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
				"query-perc99[60s]": {
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
				"query-perc90[60s]": {
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
				"query-perc99[60s]": {
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
		{
			desc: "single query with over-time aggregations via single matrix fetch",
			params: map[string]interface{}{
				"metricName":    "ActiveWatchRequests",
				"metricVersion": "v1",
				"unit":          "watches",
				"queries": []map[string]interface{}{
					{
						"query": "# dashboard comment\nsum(apiserver_longrunning_requests{verb=\"WATCH\", resource=\"pods\"})",
						"aggregations": []interface{}{
							"Perc99",
							"Perc90",
							"Perc50",
							"Max",
							"Min",
							"Avg",
							"Last",
						},
					},
				},
			},
			matrices: map[string]model.Matrix{
				"(sum(apiserver_longrunning_requests{verb=\"WATCH\", resource=\"pods\"}))[60s:30s]": {
					{
						Metric: model.Metric{},
						Values: []model.SamplePair{
							{Timestamp: 0, Value: 10},
							{Timestamp: 30000, Value: 20},
							{Timestamp: 60000, Value: 30},
						},
					},
				},
			},
			wantDataItems: []measurementutil.DataItem{
				{
					Unit: "watches",
					Data: map[string]float64{
						"Perc99": computePrometheusQuantile(0.99, []float64{10, 20, 30}),
						"Perc90": 28.0,
						"Perc50": 20.0,
						"Max":    30.0,
						"Min":    10.0,
						"Avg":    20.0,
						"Last":   30.0,
					},
				},
			},
		},
		{
			desc: "named queries with per-query aggregations and dimensions",
			params: map[string]interface{}{
				"metricName":    "APIServerPatchLatency",
				"metricVersion": "v1",
				"unit":          "s",
				"dimensions": []interface{}{
					"resource",
				},
				"queries": []map[string]interface{}{
					{
						"name":  "Spec",
						"query": "rate(apiserver_request_duration_seconds_bucket{subresource=\"\"}[1m])",
						"aggregations": []interface{}{
							"Perc99",
							"Perc50",
							"Max",
						},
					},
					{
						"name":  "Status",
						"query": "rate(apiserver_request_duration_seconds_bucket{subresource!=\"\"}[1m])",
						"aggregations": []interface{}{
							"Perc99",
							"Perc50",
							"Max",
						},
					},
				},
			},
			matrices: map[string]model.Matrix{
				"(rate(apiserver_request_duration_seconds_bucket{subresource=\"\"}[1m]))[60s:30s]": {
					{
						Metric: model.Metric{model.LabelName("resource"): model.LabelValue("pods")},
						Values: []model.SamplePair{
							{Timestamp: 0, Value: 1.0},
							{Timestamp: 30000, Value: 3.0},
						},
					},
				},
				"(rate(apiserver_request_duration_seconds_bucket{subresource!=\"\"}[1m]))[60s:30s]": {
					{
						Metric: model.Metric{model.LabelName("resource"): model.LabelValue("pods")},
						Values: []model.SamplePair{
							{Timestamp: 0, Value: 2.0},
							{Timestamp: 30000, Value: 4.0},
						},
					},
				},
			},
			wantDataItems: []measurementutil.DataItem{
				{
					Labels: map[string]string{
						"resource": "pods",
					},
					Unit: "s",
					Data: map[string]float64{
						"Spec_Perc99":   computePrometheusQuantile(0.99, []float64{1, 3}),
						"Spec_Perc50":   2.0,
						"Spec_Max":      3.0,
						"Status_Perc99": computePrometheusQuantile(0.99, []float64{2, 4}),
						"Status_Perc50": 3.0,
						"Status_Max":    4.0,
					},
				},
			},
		},
		{
			desc: "invalid aggregation name fails configuration",
			params: map[string]interface{}{
				"metricName":    "InvalidAgg",
				"metricVersion": "v1",
				"unit":          "s",
				"queries": []map[string]interface{}{
					{
						"query": "sum(my_metric)",
						"aggregations": []interface{}{
							"Perc105",
						},
					},
				},
			},
			wantConfigureErr: "invalid percentile aggregation",
		},
	}

	for i := range testCases {
		tc := &testCases[i]
		t.Run(tc.desc, func(t *testing.T) {
			gatherer := &genericQueryGatherer{}
			err := gatherer.Configure(&measurement.Config{Params: tc.params})
			if tc.wantConfigureErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantConfigureErr)
				return
			}
			assert.Nil(t, err)
			startTime := time.Now()
			endTime := startTime.Add(1 * time.Minute)
			executor := fakeQueryExecutor{
				samples:  tc.samples,
				matrices: tc.matrices,
			}

			summaries, err := gatherer.Gather(executor, startTime, endTime, nil)
			if tc.wantErr != "" {
				require.Error(t, err)
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

func TestGatherPromqlEquivalence(t *testing.T) {
	executor, err := executors.NewPromqlExecutor("testdata/generic_query_measurement/informer_cache_lag.yaml")
	require.NoError(t, err)
	defer executor.Close()

	start := time.Unix(31, 0).UTC()
	end := time.Unix(162, 0).UTC()

	aggregatedMeasurementYAML := `
Identifier: InformerCacheLagPrometheus
Method: GenericPrometheusQuery
Params:
  metricName: InformerCacheLagPrometheus
  metricVersion: v1
  unit: s
  dimensions:
  - name
  queries:
  - query: ((scalar(max(etcd_debugging_mvcc_current_revision)) - max by (name) (informer_store_resource_version{resource="pods"})) / scalar(max(deriv(etcd_debugging_mvcc_current_revision[30s])) > 0))
    aggregations:
    - Perc99
    - Perc90
    - Perc50
`

	legacyMeasurementYAML := `
Identifier: InformerCacheLagPrometheus
Method: GenericPrometheusQuery
Params:
  metricName: InformerCacheLagPrometheus
  metricVersion: v1
  unit: s
  dimensions:
  - name
  queries:
  - name: Perc99
    query: quantile_over_time(0.99, (((scalar(max(etcd_debugging_mvcc_current_revision)) - max by (name) (informer_store_resource_version{resource="pods"})) / scalar(max(deriv(etcd_debugging_mvcc_current_revision[30s])) > 0)))[%v:30s])
  - name: Perc90
    query: quantile_over_time(0.90, (((scalar(max(etcd_debugging_mvcc_current_revision)) - max by (name) (informer_store_resource_version{resource="pods"})) / scalar(max(deriv(etcd_debugging_mvcc_current_revision[30s])) > 0)))[%v:30s])
  - name: Perc50
    query: quantile_over_time(0.50, (((scalar(max(etcd_debugging_mvcc_current_revision)) - max by (name) (informer_store_resource_version{resource="pods"})) / scalar(max(deriv(etcd_debugging_mvcc_current_revision[30s])) > 0)))[%v:30s])
`

	wantJSON := `{
  "version": "v1",
  "dataItems": [
    {
      "data": {
        "Perc50": 1.5806361115725167,
        "Perc90": 4.753878227908327,
        "Perc99": 5.939070750141988
      },
      "unit": "s",
      "labels": {
        "name": "kube-apiserver"
      }
    },
    {
      "data": {
        "Perc50": 0.7331293732050739,
        "Perc90": 3.084785048170125,
        "Perc99": 3.960162983851825
      },
      "unit": "s",
      "labels": {
        "name": "kube-controller-manager"
      }
    },
    {
      "data": {
        "Perc50": 1.4117468419640744,
        "Perc90": 4.354870017180952,
        "Perc99": 5.456168610635375
      },
      "unit": "s",
      "labels": {
        "name": "kube-scheduler"
      }
    }
  ]
}`

	for _, tc := range []struct {
		name            string
		measurementYAML string
	}{
		{name: "new_aggregations_config", measurementYAML: aggregatedMeasurementYAML},
		{name: "legacy_quantile_over_time_config", measurementYAML: legacyMeasurementYAML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var parsed struct {
				Params map[string]interface{} `yaml:"Params"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(tc.measurementYAML), &parsed))

			gatherer := &genericQueryGatherer{}
			require.NoError(t, gatherer.Configure(&measurement.Config{Params: parsed.Params}))

			summaries, err := gatherer.Gather(executor, start, end, nil)
			require.NoError(t, err)
			require.Len(t, summaries, 1)

			var gotPerfData, wantPerfData measurementutil.PerfData
			require.NoError(t, json.Unmarshal([]byte(summaries[0].SummaryContent()), &gotPerfData))
			require.NoError(t, json.Unmarshal([]byte(wantJSON), &wantPerfData))

			assert.Equal(t, wantPerfData.Version, gotPerfData.Version)
			assert.ElementsMatch(t, wantPerfData.DataItems, gotPerfData.DataItems)
		})
	}
}
