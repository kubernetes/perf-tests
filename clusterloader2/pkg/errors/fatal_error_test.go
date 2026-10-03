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
	"strings"
	"testing"
)

func TestNewErrCritical(t *testing.T) {
	originalErr := errors.New("original error")
	wrappedErr := fmt.Errorf("wrapped error: %w", originalErr)
	got := NewErrCritical(wrappedErr)

	if !errors.Is(got, ErrCritical) {
		t.Errorf("errors.Is(%v, ErrCritical) = false, want true", got)
	}
	if !errors.Is(got, wrappedErr) {
		t.Errorf("errors.Is(%v, wrappedErr) = false, want true", got)
	}
	if !errors.Is(got, originalErr) {
		t.Errorf("errors.Is(%v, originalErr) = false, want true", got)
	}
	for _, want := range []string{ErrCritical.Error(), wrappedErr.Error()} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("Error() = %q, want it to contain %q", got.Error(), want)
		}
	}
}
