/*
Copyright 2025 The Kubernetes Authors.

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

package main

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsContextDoneErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	targetGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "wrapped context error is detected",
			err:      fmt.Errorf("timed out waiting for gvr %v informers to sync: %w", targetGVR, ctx.Err()),
			expected: true,
		},
		{
			name:     "unwrapped error is not detected",
			err:      fmt.Errorf("timed out waiting for gvr %v informers to sync", targetGVR),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isContextDoneErr(tt.err); got != tt.expected {
				t.Errorf("isContextDoneErr(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
