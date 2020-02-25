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

package util

import (
	"math"
	"reflect"
	"sort"
	"strconv"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"

	"k8s.io/component-base/metrics/testutil"
)

// Histogram is a structure that represents distribution of data.
type Histogram struct {
	Labels  map[string]string `json:"labels"`
	Buckets map[string]int    `json:"buckets"`
}

// Quantile calculates the quantile 'q' based on the given buckets of Histogram.
func (h *Histogram) Quantile(q float64) (float64, error) {
	hist := testutil.Histogram{
		&dto.Histogram{
			Bucket: []*dto.Bucket{},
		},
	}

	for k, v := range h.Buckets {
		upper, err := strconv.ParseFloat(k, 64)
		if err != nil {
			return math.MaxFloat64, err
		}

		cumulativeCount := uint64(v)
		hist.Bucket = append(hist.Bucket, &dto.Bucket{
			CumulativeCount: &cumulativeCount,
			UpperBound:      &upper,
		})
	}

	// hist.Quantile expected a slice of Buckets ordered by UpperBound
	sort.Slice(hist.Bucket, func(i, j int) bool {
		return *hist.Bucket[i].UpperBound < *hist.Bucket[j].UpperBound
	})

	return hist.Quantile(q), nil
}

// HistogramVec is an array of Histograms.
type HistogramVec []Histogram

// NewHistogram creates new Histogram instance.
func NewHistogram(labels map[string]string) *Histogram {
	return &Histogram{
		Labels:  labels,
		Buckets: make(map[string]int),
	}
}

// ConvertSampleToBucket converts prometheus sample into HistogramVec bucket.
func ConvertSampleToBucket(sample *model.Sample, h *HistogramVec) {
	labels := make(map[string]string)
	for k, v := range sample.Metric {
		if k != model.BucketLabel {
			labels[string(k)] = string(v)
		}
	}
	var hist *Histogram
	for i := range *h {
		if reflect.DeepEqual(labels, (*h)[i].Labels) {
			hist = &((*h)[i])
			break
		}
	}
	if hist == nil {
		hist = NewHistogram(labels)
		*h = append(*h, *hist)
	}
	hist.Buckets[string(sample.Metric[model.BucketLabel])] += int(sample.Value)
}

// ConvertSampleToHistogram converts prometheus sample into Histogram.
func ConvertSampleToHistogram(sample *model.Sample, h *Histogram) {
	labels := make(map[string]string)
	for k, v := range sample.Metric {
		if k != model.BucketLabel {
			labels[string(k)] = string(v)
		}
	}

	h.Labels = labels
	h.Buckets[string(sample.Metric[model.BucketLabel])] += int(sample.Value)
}
