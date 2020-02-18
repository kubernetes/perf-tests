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

// TestHistogramQuantile makes sure sorted/unsorted list of samples
// gives the same percentiles
func TestHistogramQuantile(t *testing.T) {
	tests := []struct {
		histogram Histogram
		q50       float64
		q90       float64
		q99       float64
	}{
		// unsorted sequence
		{
			histogram: Histogram{
				Buckets: map[string]int{
					"4": 10,
					"8": 20,
					"1": 20,
					"2": 10,
				},
			},
			q50: 0.5,
			q90: 7.2,
			q99: 7.92,
		},
		// unsorted sequence
		{
			histogram: Histogram{
				Buckets: map[string]int{
					"1": 20,
					"2": 10,
					"4": 10,
					"8": 20,
				},
			},
			q50: 0.5,
			q90: 7.2,
			q99: 7.92,
		},
	}

	for _, test := range tests {
		if q50, err := test.histogram.Quantile(0.5); err != nil {
			t.Errorf("Unexpected error for q50: %v", err)
		} else if q50 != test.q50 {
			t.Errorf("Expected q50 to be %v, got %v instead", test.q50, q50)
		}

		if q90, err := test.histogram.Quantile(0.9); err != nil {
			t.Errorf("Unexpected error for q90: %v", err)
		} else if q90 != test.q90 {
			t.Errorf("Expected q90 to be %v, got %v instead", test.q90, q90)
		}

		if q99, err := test.histogram.Quantile(0.99); err != nil {
			t.Errorf("Unexpected error for q99: %v", err)
		} else if q99 != test.q99 {
			t.Errorf("Expected q99 to be %v, got %v instead", test.q99, q99)
		}
	}
}
