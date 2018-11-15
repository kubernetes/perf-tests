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
	"fmt"
	"time"
)

// LatencyMetric represent 50th, 90th and 99th duration quantiles.
type LatencyMetric struct {
	Perc50 time.Duration `json:"Perc50"`
	Perc90 time.Duration `json:"Perc90"`
	Perc99 time.Duration `json:"Perc99"`
}

// SetQuantile set quantile value.
// Only 0.5, 0.9 and 0.99 quantiles are supported.
func (metric *LatencyMetric) SetQuantile(quantile float64, latency time.Duration) {
	switch quantile {
	case 0.5:
		metric.Perc50 = latency
	case 0.9:
		metric.Perc90 = latency
	case 0.99:
		metric.Perc99 = latency
	}
}

// VerifyThreshod verifies latency metric against given percentile thresholds.
func (metric *LatencyMetric) VerifyThreshod(threshold *LatencyMetric) error {
	if metric.Perc50 > threshold.Perc50 {
		return fmt.Errorf("too high latency 50th percentile: %v", metric.Perc50)
	}
	if metric.Perc90 > threshold.Perc90 {
		return fmt.Errorf("too high latency 90th percentile: %v", metric.Perc90)
	}
	if metric.Perc99 > threshold.Perc99 {
		return fmt.Errorf("too high latency 99th percentile: %v", metric.Perc99)
	}
	return nil
}
