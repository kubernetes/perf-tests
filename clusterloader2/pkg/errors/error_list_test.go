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
	"fmt"
	"testing"
)

func TestNewErrorList(t *testing.T) {
	errOne := errors.New("first error")
	errTwo := errors.New("second error")
	tests := []struct {
		name      string
		errors    []error
		wantEmpty bool
		want      string
	}{
		{
			name:      "empty",
			wantEmpty: true,
			want:      "[]",
		},
		{
			name:      "with errors",
			errors:    []error{errOne, errTwo},
			wantEmpty: false,
			want:      "[first error\nsecond error]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewErrorList(tt.errors...)
			if got := list.IsEmpty(); got != tt.wantEmpty {
				t.Errorf("IsEmpty() = %t, want %t", got, tt.wantEmpty)
			}
			if got := list.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := list.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorListAppend(t *testing.T) {
	tests := []struct {
		name string
		errs []error
		want string
	}{
		{
			name: "single error",
			errs: []error{errors.New("first error")},
			want: "[first error]",
		},
		{
			name: "multiple errors",
			errs: []error{errors.New("first error"), errors.New("second error")},
			want: "[first error\nsecond error]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewErrorList()
			list.Append(tt.errs...)

			if list.IsEmpty() {
				t.Error("IsEmpty() = true, want false")
			}
			if got := list.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorListConcat(t *testing.T) {
	errOne := errors.New("first error")
	errTwo := errors.New("second error")
	tests := []struct {
		name  string
		base  []error
		other *ErrorList
		want  string
	}{
		{
			name:  "nil list",
			base:  []error{errOne},
			other: nil,
			want:  "[first error]",
		},
		{
			name:  "empty list",
			base:  []error{errOne},
			other: NewErrorList(),
			want:  "[first error]",
		},
		{
			name:  "non-empty list",
			base:  []error{errOne},
			other: NewErrorList(errTwo),
			want:  "[first error\nsecond error]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := NewErrorList(tt.base...)
			list.Concat(tt.other)

			if got := list.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorListHas(t *testing.T) {
	directErr := errors.New("direct error")
	wrappedErr := errors.New("wrapped error")
	list := NewErrorList(directErr, fmt.Errorf("context: %w", wrappedErr))

	tests := []struct {
		name   string
		target error
		want   bool
	}{
		{
			name:   "direct match",
			target: directErr,
			want:   true,
		},
		{
			name:   "wrapped match",
			target: wrappedErr,
			want:   true,
		},
		{
			name:   "different error",
			target: errors.New("different error"),
			want:   false,
		},
		{
			name:   "same message but different error",
			target: errors.New("direct error"),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := list.Has(tt.target); got != tt.want {
				t.Errorf("Has(%v) = %t, want %t", tt.target, got, tt.want)
			}
		})
	}
}
