/*
Copyright 2026 The Kubernetes Authors.

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

import (
	"errors"
	"testing"
)

func TestNewMetricViolationError(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		reason string
		want   string
	}{
		{
			name:   "metric violation",
			metric: "pod startup latency",
			reason: "threshold exceeded",
			want:   "pod startup latency: threshold exceeded",
		},
		{
			name:   "empty values",
			metric: "",
			reason: "",
			want:   ": ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewMetricViolationError(tt.metric, tt.reason).Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsMetricViolationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "metric violation error",
			err:  NewMetricViolationError("metric", "reason"),
			want: true,
		},
		{
			name: "standard error",
			err:  errors.New("standard error"),
			want: false,
		},
		{
			name: "critical error",
			err:  NewErrCritical(errors.New("critical error")),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMetricViolationError(tt.err); got != tt.want {
				t.Errorf("IsMetricViolationError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
