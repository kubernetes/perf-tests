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

	"github.com/prometheus/common/model"
)

// Bucket of a histogram
type bucket struct {
	upperBound float64
	count      float64
}

type buckets []bucket

func (b buckets) Len() int           { return len(b) }
func (b buckets) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b buckets) Less(i, j int) bool { return b[i].upperBound < b[j].upperBound }

// bucketQuantile return Quantile of calculate from buckets
// copy from https://github.com/kubernetes/kubernetes/pull/85861/files#diff-8fc16044937fc03c39b585a657b20150R179-R209
func bucketQuantile(q float64, buckets buckets) float64 {
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}

	if len(buckets) < 2 {
		return math.NaN()
	}

	rank := q * buckets[len(buckets)-1].count
	b := sort.Search(len(buckets)-1, func(i int) bool { return buckets[i].count >= rank })

	if b == 0 {
		return buckets[0].upperBound * (rank / buckets[0].count)
	}

	// linear approximation of b-th bucket
	brank := rank - buckets[b-1].count
	bSize := buckets[b].upperBound - buckets[b-1].upperBound
	bCount := buckets[b].count - buckets[b-1].count

	return buckets[b-1].upperBound + bSize*(brank/bCount)
}

// Histogram is a structure that represents distribution of data.
type Histogram struct {
	Labels  map[string]string `json:"labels"`
	Buckets map[string]int    `json:"buckets"`
}

// Quantile calculates the quantile 'q' based on the given buckets of Histogram.
func (h *Histogram) Quantile(q float64) (float64, error) {
	var b []bucket
	for k, v := range h.Buckets {
		upper, err := strconv.ParseFloat(k, 64)
		if err != nil {
			return math.MaxFloat64, err
		}
		b = append(b, bucket{
			upperBound: upper,
			count:      float64(v),
		})
	}

	sort.Sort(buckets(b))
	return bucketQuantile(q, b), nil
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
