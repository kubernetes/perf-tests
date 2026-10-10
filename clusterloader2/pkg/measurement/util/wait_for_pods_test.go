/*
Copyright The Kubernetes Authors.

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
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakePodLister struct {
	lock sync.Mutex
	pods []*corev1.Pod
}

func (f *fakePodLister) List() ([]*corev1.Pod, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.pods, nil
}

func (f *fakePodLister) setPods(pods []*corev1.Pod) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.pods = pods
}

func (f *fakePodLister) String() string {
	return "fakePodStore"
}

func runningAndReadyPodWithLabels(name string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func TestWaitForPods_WithinTolerationTimeout(t *testing.T) {
	lister := &fakePodLister{
		pods: []*corev1.Pod{
			runningAndReadyPodWithLabels("pod-1", map[string]string{"app": "test"}),
		},
	}

	options := &WaitForPodOptions{
		DesiredPodCount:     func() int { return 1 },
		CallerName:          "Test",
		WaitForPodsInterval: 10 * time.Millisecond,
		TolerationTimeout:   200 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	status, err := WaitForPods(ctx, lister, options)
	if err != nil {
		t.Fatalf("expected nil error when pods are ready before tolerationTimeout, got: %v", err)
	}
	if status != nil {
		t.Fatalf("expected nil status when pods are ready before tolerationTimeout, got: %v", status)
	}
}

func TestWaitForPods_AfterTolerationTimeoutBeforeContextDeadline(t *testing.T) {
	lister := &fakePodLister{
		pods: []*corev1.Pod{},
	}

	options := &WaitForPodOptions{
		DesiredPodCount:     func() int { return 1 },
		CallerName:          "Test",
		WaitForPodsInterval: 20 * time.Millisecond,
		TolerationTimeout:   100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Add ready pod AFTER tolerationTimeout elapses (at ~200ms)
	time.AfterFunc(200*time.Millisecond, func() {
		lister.setPods([]*corev1.Pod{
			runningAndReadyPodWithLabels("pod-1", map[string]string{"app": "test"}),
		})
	})

	status, err := WaitForPods(ctx, lister, options)
	if err == nil {
		t.Fatalf("expected error when pods become ready after tolerationTimeout, got nil")
	}
	if status != nil {
		t.Fatalf("expected nil status when pods successfully become ready after tolerationTimeout, got: %v", status)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "reached after tolerationTimeout") {
		t.Errorf("expected error message to contain 'reached after tolerationTimeout', got: %v", errMsg)
	}
	if !strings.Contains(errMsg, "delay after tolerationTimeout") {
		t.Errorf("expected error message to contain 'delay after tolerationTimeout', got: %v", errMsg)
	}
}

func TestWaitForPods_TimeoutAfterContextDeadline(t *testing.T) {
	lister := &fakePodLister{
		pods: []*corev1.Pod{},
	}

	options := &WaitForPodOptions{
		DesiredPodCount:     func() int { return 1 },
		CallerName:          "Test",
		WaitForPodsInterval: 20 * time.Millisecond,
		TolerationTimeout:   100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	status, err := WaitForPods(ctx, lister, options)
	if err == nil {
		t.Fatalf("expected timeout error when pods never become ready, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "while waiting for 1 pods to be running") {
		t.Errorf("expected standard timeout error message, got: %v", errMsg)
	}
	if status == nil {
		t.Fatalf("expected non-nil status on timeout, got nil")
	}
}

func pendingPodWithLabels(name string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func TestCalculateDesiredPodRange(t *testing.T) {
	testCases := []struct {
		name                string
		params              map[string]interface{}
		initialRunningCount int
		expectedMin         int
		expectedMax         int
		expectedMargin      int
		expectErr           bool
	}{
		{
			name:                "no difference specified (exact count)",
			params:              map[string]interface{}{},
			initialRunningCount: 100,
			expectedMin:         100,
			expectedMax:         100,
			expectedMargin:      0,
		},
		{
			name: "toleration 1%",
			params: map[string]interface{}{
				"toleration": 1.0,
			},
			initialRunningCount: 1000,
			expectedMin:         990,
			expectedMax:         1010,
			expectedMargin:      10,
		},
		{
			name: "toleration 5%",
			params: map[string]interface{}{
				"toleration": 5.0,
			},
			initialRunningCount: 200,
			expectedMin:         190,
			expectedMax:         210,
			expectedMargin:      10,
		},
		{
			name: "toleration with decimal places 1.5%",
			params: map[string]interface{}{
				"toleration": 1.5,
			},
			initialRunningCount: 1000,
			expectedMin:         985,
			expectedMax:         1015,
			expectedMargin:      15,
		},
		{
			name: "toleration with decimal places ceiling rounding",
			params: map[string]interface{}{
				"toleration": 0.25,
			},
			initialRunningCount: 100,
			expectedMin:         99,
			expectedMax:         101,
			expectedMargin:      1,
		},
		{
			name: "explicit minDesiredPodCount and maxDesiredPodCount",
			params: map[string]interface{}{
				"minDesiredPodCount": 80,
				"maxDesiredPodCount": 120,
			},
			initialRunningCount: 100,
			expectedMin:         80,
			expectedMax:         120,
			expectedMargin:      20,
		},
		{
			name: "invalid range min > max",
			params: map[string]interface{}{
				"minDesiredPodCount": 120,
				"maxDesiredPodCount": 80,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "only minDesiredPodCount specified",
			params: map[string]interface{}{
				"minDesiredPodCount": 80,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "only maxDesiredPodCount specified",
			params: map[string]interface{}{
				"maxDesiredPodCount": 120,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "both min/max and toleration specified",
			params: map[string]interface{}{
				"minDesiredPodCount": 80,
				"maxDesiredPodCount": 120,
				"toleration":         5.0,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "toleration out of range negative",
			params: map[string]interface{}{
				"toleration": -1.0,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "toleration out of range > 100",
			params: map[string]interface{}{
				"toleration": 100.1,
			},
			initialRunningCount: 100,
			expectErr:           true,
		},
		{
			name: "initial count 0 with toleration",
			params: map[string]interface{}{
				"toleration": 1.0,
			},
			initialRunningCount: 0,
			expectedMin:         0,
			expectedMax:         0,
			expectedMargin:      0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			minDesired, maxDesired, margin, err := CalculateDesiredPodRange(tc.params, tc.initialRunningCount)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if minDesired != tc.expectedMin {
				t.Errorf("minDesired = %d, want %d", minDesired, tc.expectedMin)
			}
			if maxDesired != tc.expectedMax {
				t.Errorf("maxDesired = %d, want %d", maxDesired, tc.expectedMax)
			}
			if margin != tc.expectedMargin {
				t.Errorf("margin = %d, want %d", margin, tc.expectedMargin)
			}
		})
	}
}

func TestWaitForPods_WithToleration(t *testing.T) {
	// Initially 100 pods, ±2% toleration -> [98, 102]
	pods99 := make([]*corev1.Pod, 99)
	for i := 0; i < 99; i++ {
		pods99[i] = runningAndReadyPodWithLabels(string(rune('a'+i)), map[string]string{"app": "foo"})
	}

	lister := &fakePodLister{pods: pods99}
	tol := 2.0
	options := &WaitForPodOptions{
		DesiredPodCount:     func() int { return 100 },
		Toleration:          &tol,
		CallerName:          "TestToleration",
		WaitForPodsInterval: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := WaitForPods(ctx, lister, options)
	if err != nil {
		t.Fatalf("expected success when running pods (99) is within range [98, 102], got: %v", err)
	}
}

func TestWaitForPods_WithMinMaxRange(t *testing.T) {
	pods := []*corev1.Pod{
		runningAndReadyPodWithLabels("pod-1", nil),
		runningAndReadyPodWithLabels("pod-2", nil),
	}
	lister := &fakePodLister{pods: pods}
	minPod := 1
	maxPod := 3
	options := &WaitForPodOptions{
		MinDesiredPodCount:  &minPod,
		MaxDesiredPodCount:  &maxPod,
		CallerName:          "TestMinMax",
		WaitForPodsInterval: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := WaitForPods(ctx, lister, options)
	if err != nil {
		t.Fatalf("expected success when running pods (2) is within range [1, 3], got: %v", err)
	}
}

func TestWaitForPods_CheckPodsRunningFalse(t *testing.T) {
	// Include both Running and Pending pods; when CheckPodsRunning is false, only len(pods) matters.
	pods := []*corev1.Pod{
		runningAndReadyPodWithLabels("pod-1", nil),
		pendingPodWithLabels("pod-2", nil),
		pendingPodWithLabels("pod-3", nil),
	}
	lister := &fakePodLister{pods: pods}
	checkRunning := false
	tol := 25.0 // 4 ± ceil(4 * 0.25) = [3, 5]
	options := &WaitForPodOptions{
		DesiredPodCount:     func() int { return 4 },
		Toleration:          &tol,
		CheckPodsRunning:    &checkRunning,
		CallerName:          "TestCountOnly",
		WaitForPodsInterval: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := WaitForPods(ctx, lister, options)
	if err != nil {
		t.Fatalf("expected success when total pods (3, including pending) is within range [3, 5] with CheckPodsRunning=false, got: %v", err)
	}
}

