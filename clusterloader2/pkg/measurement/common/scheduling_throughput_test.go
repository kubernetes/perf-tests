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

package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scheduledPod(unixSecond int64) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Unix(unixSecond, 0)),
			}},
		},
	}
}

func unscheduledPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionFalse,
			}},
		},
	}
}

func TestComputeSchedulingThroughput(t *testing.T) {
	cases := []struct {
		name        string
		pods        []*corev1.Pod
		startSecond int64
		wantTotal   int
		wantWindow  int
		wantPerSec  []int
		wantP50     float64
		wantMax     float64
	}{
		{
			name:        "buckets pods by their scheduled second",
			pods:        []*corev1.Pod{scheduledPod(100), scheduledPod(100), scheduledPod(100), scheduledPod(101), scheduledPod(102), scheduledPod(102)},
			startSecond: 0,
			wantTotal:   6,
			wantWindow:  3,
			wantPerSec:  []int{3, 1, 2},
			wantP50:     2,
			wantMax:     3,
		},
		{
			name:        "ignores pods scheduled before start",
			pods:        []*corev1.Pod{scheduledPod(90), scheduledPod(95), scheduledPod(100), scheduledPod(100)},
			startSecond: 100,
			wantTotal:   2,
			wantWindow:  1,
			wantPerSec:  []int{2},
			wantP50:     2,
			wantMax:     2,
		},
		{
			name:        "skips unscheduled pods",
			pods:        []*corev1.Pod{scheduledPod(100), unscheduledPod(), {}},
			startSecond: 0,
			wantTotal:   1,
			wantWindow:  1,
			wantPerSec:  []int{1},
			wantP50:     1,
			wantMax:     1,
		},
		{
			name:        "zero-fills idle seconds inside the window",
			pods:        []*corev1.Pod{scheduledPod(100), scheduledPod(100), scheduledPod(102)},
			startSecond: 0,
			wantTotal:   3,
			wantWindow:  3,
			wantPerSec:  []int{2, 0, 1},
			wantP50:     1,
			wantMax:     2,
		},
		{
			name:        "no scheduled pods",
			pods:        []*corev1.Pod{unscheduledPod(), {}},
			startSecond: 0,
			wantTotal:   0,
			wantWindow:  0,
			wantPerSec:  nil,
			wantP50:     0,
			wantMax:     0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeSchedulingThroughput(tc.pods, tc.startSecond)
			assert.Equal(t, tc.wantTotal, got.TotalScheduled, "totalScheduled")
			assert.Equal(t, tc.wantWindow, got.WindowSeconds, "windowSeconds")
			assert.Equal(t, tc.wantP50, got.Perc50, "perc50")
			assert.Equal(t, tc.wantMax, got.Max, "max")
			var gotPerSec []int
			for _, p := range got.PerSecond {
				gotPerSec = append(gotPerSec, p.Scheduled)
			}
			assert.Equal(t, tc.wantPerSec, gotPerSec, "perSecond counts")
		})
	}
}
