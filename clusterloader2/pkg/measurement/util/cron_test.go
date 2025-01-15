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

package util

import (
	"runtime"
	"testing"
	"time"
)

type mockTicker struct {
	C   chan time.Time
	now time.Time
}

func newMockTicker() *mockTicker {
	return &mockTicker{
		C:   make(chan time.Time, 5),
		now: time.Now(),
	}
}

func (t *mockTicker) tick() {
	t.now = t.now.Add(time.Minute)
	t.C <- t.now
}

func TestRunEveryTick(t *testing.T) {
	tests := []struct {
		name     string
		stop     bool
		expected int
	}{
		{
			name:     "WithoutStop",
			stop:     false,
			expected: 2,
		},
		{
			name:     "WithStop",
			stop:     true,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := 0
			increase := func() {
				counter++
			}
			ticker := newMockTicker()
			stop := make(chan struct{})
			go RunEveryTick(&time.Ticker{C: ticker.C}, increase, stop)

			ticker.tick()
			runtime.Gosched()

			if tt.stop {
				close(stop)
				runtime.Gosched()
			}

			ticker.tick()
			runtime.Gosched()

			if counter != tt.expected {
				t.Errorf("Expect counter to be %d, got %d", tt.expected, counter)
			}
		})
	}
}
