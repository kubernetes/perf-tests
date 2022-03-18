/*
Copyright 2019 The Kubernetes Authors.

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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

func TestGather(t *testing.T) {
	cases := []struct {
		samples   []*model.Sample
		err       error
		wantData  *measurementutil.PerfData
		wantError error
	}{{
		samples:  []*model.Sample{createSample("0.9", 200.5), createSample("0.5", 100.5), createSample("0.99", 300.5)},
		wantData: createPerfData([]float64{100500, 200500, 300500}),
	}, {
		samples:   []*model.Sample{{Value: 1}},
		wantError: errors.New("got unexpected number of samples: 1"),
	}, {
		samples:   []*model.Sample{{Value: 1}, {Value: 2}, {Value: 3}, {Value: 4}},
		wantError: errors.New("got unexpected number of samples: 4"),
	}}

	for _, v := range cases {
		fakeExecutor := &fakeExecutor{samples: v.samples, err: v.err}
		testGatherer(t, fakeExecutor, v.wantData, v.wantError)
	}
}

func testGatherer(t *testing.T, executor common.QueryExecutor, wantData *measurementutil.PerfData, wantError error) {
	g := &netProgGatherer{}
	summaries, err := g.Gather(executor, time.Now(), time.Now(), nil)
	if err != nil {
		if wantError != nil {
			assert.Equal(t, wantError, err)
			return
		}
		t.Errorf("Unexpected error:  %v", err)
	}
	if len(summaries) != 1 {
		t.Errorf("Should have only one summary")
	}
	assert.Equal(t, netProg, summaries[0].SummaryName())
	assert.Equal(t, "json", summaries[0].SummaryExt())
	assert.NotNil(t, summaries[0].SummaryTime())

	var data measurementutil.PerfData
	if err := json.Unmarshal([]byte(summaries[0].SummaryContent()), &data); err != nil {
		t.Errorf("Error while decoding summary: %v. Summary: %v", err, summaries[0].SummaryContent())

	}
	assert.Equal(t, wantData, &data)
}

type fakeExecutor struct {
	samples []*model.Sample
	err     error
}

func (f *fakeExecutor) Query(query string, queryTime time.Time) ([]*model.Sample, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.samples, nil
}

func createSample(p string, l float64) *model.Sample {
	lset := make(model.LabelSet, 1)
	lset["quantile"] = model.LabelValue(p)
	return &model.Sample{
		Value:  model.SampleValue(l),
		Metric: model.Metric(lset),
	}
}

func createPerfData(p []float64) *measurementutil.PerfData {
	return &measurementutil.PerfData{
		Version: metricVersion,
		DataItems: []measurementutil.DataItem{{
			Data: map[string]float64{
				"Perc50": p[0],
				"Perc90": p[1],
				"Perc99": p[2],
			},
			Unit:   "ms",
			Labels: map[string]string{"Metric": "NetworkProgrammingLatency"},
		}},
	}
}
