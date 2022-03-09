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
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

type fakeQueryExecutor struct {
	samples map[string][]float64
}

func (f fakeQueryExecutor) Query(query string, _ time.Time) ([]*model.Sample, error) {
	samples, found := f.samples[query]
	if !found {
		return nil, nil
	}
	res := []*model.Sample{}
	for _, s := range samples {
		res = append(res, &model.Sample{Value: model.SampleValue(s)})
	}
	return res, nil
}

func TestGather(t *testing.T) {
	testCases := []struct {
		desc             string
		params           map[string]interface{}
		samples          map[string][]float64
		expectedData     map[string]float64
		notWantedData    []string
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
				},
			},
			samples: map[string][]float64{
				"below-threshold-query[1m]": {7},
				"no-threshold-query[1m]":    {120},
			},
			expectedData: map[string]float64{
				"below-threshold": 7.0,
				"no-threshold":    120.0,
			},
			notWantedData: []string{
				"no-samples",
			},
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
			samples: map[string][]float64{
				"many-samples-query[1m]": {1, 2, 3, 4, 5},
			},
			wantErr: "too many samples",
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
			samples: map[string][]float64{
				"above-threshold-query[1m]": {113},
			},
			wantErr: "sample above threshold",
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
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gatherer := &genericQueryGatherer{}
			err := gatherer.Configure(&measurement.Config{Params: tc.params})
			if tc.wantConfigureErr != "" {
				assert.EqualError(t, err, tc.wantConfigureErr)
				return
			}
			assert.Nil(t, err)
			startTime := time.Now()
			endTime := startTime.Add(1 * time.Minute)
			executor := fakeQueryExecutor{tc.samples}

			summaries, err := gatherer.Gather(executor, startTime, endTime, nil)
			if err != nil {
				if tc.wantErr != "" {
					assert.True(t, strings.Contains(err.Error(), tc.wantErr), "unexpected err: got %v, want: %v", err, tc.wantErr)
				} else {
					t.Fatalf("got: %v, want no error", err)
				}
			}

			if len(summaries) != 1 {
				t.Fatalf("wrong number of summaries, got: %v, want: 1", len(summaries))
			}
			content := summaries[0].SummaryContent()
			for k, v := range tc.expectedData {
				entry := fmt.Sprintf("\"%v\": %v", k, v)
				assert.True(t, strings.Contains(content, entry), "summary missing data: got: %v, want: %v", content, entry)
			}
			for _, s := range tc.notWantedData {
				assert.False(t, strings.Contains(content, s), "summary contains extra data: got: %v, want: no %v", content, s)
			}
			name := tc.params["metricName"].(string)
			metricNameLabelPattern := fmt.Sprintf("\"labels\":\\s*\\{\\s*\"MetricName\":\\s*\"%v\"\\s*\\}", name)
			if match, err := regexp.MatchString(metricNameLabelPattern, content); !match {
				if err != nil {
					t.Errorf("failed to match MetricName label, got err: %v", err)
				} else {
					t.Errorf("MetricName label not matched, got: %v, wanted to match, %v", content, metricNameLabelPattern)
				}
			}
		})
	}
}
