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

package errors

type metricViolationError struct {
	metric string
	reason string
}

func (m *metricViolationError) Error() string {
	return m.metric + ": " + m.reason
}

// NewMetricViolationError creates new metric violation error.
func NewMetricViolationError(metric, reason string) error {
	return &metricViolationError{
		metric: metric,
		reason: reason,
	}
}

// IsMetricViolationError checks if given error is MetricViolation type.
func IsMetricViolationError(err error) bool {
	_, ok := err.(*metricViolationError)
	return ok
}
