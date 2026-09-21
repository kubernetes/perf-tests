/*
Copyright 2024 The Kubernetes Authors.

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

package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func TestClusterOOMsTrackerMeasurementGather(t *testing.T) {
	cases := []struct {
		name          string
		config        *measurement.Config
		ooms          []oomEvent
		expectedError bool
	}{
		{
			name: "no ooms",
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
			ooms:          []oomEvent{},
			expectedError: false,
		},
		{
			name: "ooms with failure enabled (default)",
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
			ooms: []oomEvent{
				{
					Process: "test-process",
					Time:    time.Now().Add(time.Minute), // future time so it's not "past"
				},
			},
			expectedError: true,
		},
		{
			name: "ooms with failure disabled",
			config: &measurement.Config{
				Params: map[string]interface{}{
					"clusterOOMsFailureEnabled": false,
				},
			},
			ooms: []oomEvent{
				{
					Process: "test-process",
					Time:    time.Now().Add(time.Minute),
				},
			},
			expectedError: false,
		},
		{
			name: "ignored processes don't cause failure",
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
			ooms: []oomEvent{
				{
					Process: "ignored-process",
					Time:    time.Now().Add(time.Minute),
				},
			},
			expectedError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &clusterOOMsTrackerMeasurement{
				isRunning: true,
				startTime: time.Now(),
				stopCh:    make(chan struct{}),
				processIgnored: map[string]bool{
					"ignored-process": true,
				},
				ooms: tc.ooms,
			}

			summaries, err := m.gather(tc.config)
			if tc.expectedError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
			assert.Len(t, summaries, 1)
			assert.Equal(t, clusterOOMsTrackerName, summaries[0].SummaryName())
		})
	}
}
