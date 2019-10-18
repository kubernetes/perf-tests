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

package util

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
)

func TestConvertSampleToBucket(t *testing.T) {
	tests := map[string]struct {
		samples []*model.Sample
		want    *HistogramVec
	}{
		"convert single sample to bucket": {
			samples: []*model.Sample{
				{
					Metric: model.Metric{"name": "value", "le": "le-value"},
					Value:  1,
				},
			},
			want: &HistogramVec{
				{
					Labels: map[string]string{
						"name": "value",
					},
					Buckets: map[string]int{
						"le-value": 1,
					},
				},
			},
		},
		"convert multiple samples to bucket": {
			samples: []*model.Sample{
				{
					Metric: model.Metric{"name": "value", "le": "le-value"},
					Value:  1,
				},
				{
					Metric: model.Metric{"name": "value", "le": "le-value"},
					Value:  2,
				},
			},
			want: &HistogramVec{
				{
					Labels: map[string]string{
						"name": "value",
					},
					Buckets: map[string]int{
						"le-value": 3,
					},
				},
			},
		},
	}

	var inputHistogramVec HistogramVec
	for name, test := range tests {
		inputHistogramVec = HistogramVec{
			{
				Labels: map[string]string{
					"name": "value",
				},
				Buckets: map[string]int{
					"le-value": 0,
				},
			},
		}
		for _, sample := range test.samples {
			ConvertSampleToBucket(sample, &inputHistogramVec)
		}
		if !reflect.DeepEqual(&inputHistogramVec, test.want) {
			t.Errorf("error %s: \n\tgot %#v \n\twanted %#v", name, &inputHistogramVec, test.want)
		}
	}
}
