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
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

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
			name: "allowedDifferencePercentage 1%",
			params: map[string]interface{}{
				"allowedDifferencePercentage": 1.0,
			},
			initialRunningCount: 1000,
			expectedMin:         990,
			expectedMax:         1010,
			expectedMargin:      10,
		},
		{
			name: "tolerancePercentage 5%",
			params: map[string]interface{}{
				"tolerancePercentage": 5.0,
			},
			initialRunningCount: 200,
			expectedMin:         190,
			expectedMax:         210,
			expectedMargin:      10,
		},
		{
			name: "tolerationPercentage 2%",
			params: map[string]interface{}{
				"tolerationPercentage": 2.0,
			},
			initialRunningCount: 100,
			expectedMin:         98,
			expectedMax:         102,
			expectedMargin:      2,
		},
		{
			name: "allowedDifferenceRatio 0.01 (1%)",
			params: map[string]interface{}{
				"allowedDifferenceRatio": 0.01,
			},
			initialRunningCount: 100,
			expectedMin:         99,
			expectedMax:         101,
			expectedMargin:      1,
		},
		{
			name: "countErrorMargin 5",
			params: map[string]interface{}{
				"countErrorMargin": 5,
			},
			initialRunningCount: 50,
			expectedMin:         45,
			expectedMax:         55,
			expectedMargin:      5,
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
			name: "initial count 0 with percentage",
			params: map[string]interface{}{
				"allowedDifferencePercentage": 1.0,
			},
			initialRunningCount: 0,
			expectedMin:         0,
			expectedMax:         0,
			expectedMargin:      0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			minDesired, maxDesired, margin, err := calculateDesiredPodRange(tc.params, tc.initialRunningCount)
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

func createTestRunningPod(name, namespace string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
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

func createTestPendingPod(name, namespace string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func createTestTerminatingPod(name, namespace string, labels map[string]string) *corev1.Pod {
	now := metav1.Now()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            labels,
			DeletionTimestamp: &now,
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

func TestWaitForRunningPodsRestart_Lifecycle(t *testing.T) {
	objects := []runtime.Object{
		createTestRunningPod("pod-1", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-2", "test-ns", map[string]string{"app": "foo"}),
		createTestPendingPod("pod-3", "test-ns", map[string]string{"app": "foo"}),
		createTestTerminatingPod("pod-4", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-other", "test-ns", map[string]string{"app": "bar"}),
	}

	fakeClient := fake.NewSimpleClientset(objects...)
	multiClientSet := framework.NewMultiClientSetFromClients(fakeClient)
	clusterFramework := framework.NewFrameworkFromClients(multiClientSet, nil)

	m := createWaitForRunningPodsRestartMeasurementFactory("WaitForRunningPodsRestart")()

	// 1. Gather before start should error
	_, err := m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action": "gather",
		},
	})
	if err == nil {
		t.Fatalf("expected error when calling gather before start, got nil")
	}

	// 2. Start should count all pods matching selector
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action":        "start",
			"namespace":     "test-ns",
			"labelSelector": "app=foo",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error on start: %v", err)
	}

	measInstance := m.(*waitForRunningPodsRestartMeasurement)
	if measInstance.totalPodsCount != 4 {
		t.Fatalf("expected 4 total pods counted on start, got %d", measInstance.totalPodsCount)
	}

	// 3. Stop should reset state
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action": "stop",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error on stop: %v", err)
	}
	if measInstance.isRunning {
		t.Fatalf("expected isRunning to be false after stop")
	}

	// 4. Unknown action should error
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action": "invalid",
		},
	})
	if err == nil {
		t.Fatalf("expected error for invalid action, got nil")
	}
}

func TestWaitForRunningPodsRestart_WaitForPodsWithDifference(t *testing.T) {
	// Initially 100 pods, ±2% difference -> [98, 102]
	// If cluster has 99 running pods (less by 1%), it should succeed.
	pods99 := make([]*corev1.Pod, 99)
	for i := 0; i < 99; i++ {
		pods99[i] = createTestRunningPod(string(rune('a'+i)), "test-ns", map[string]string{"app": "foo"})
	}

	lister := &fakePodLister{pods: pods99}
	w := &waitForRunningPodsRestartMeasurement{
		callerName: "TestWait",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := w.waitForPods(ctx, lister, 98, 102, 100, 10*time.Millisecond, 0)
	if err != nil {
		t.Fatalf("expected success when running pods (99) is within range [98, 102], got: %v", err)
	}
}

func TestIsPodsStatusAcceptable(t *testing.T) {
	podRunning1 := createTestRunningPod("pod-1", "test-ns", nil)
	podRunning2 := createTestRunningPod("pod-2", "test-ns", nil)
	podPending := createTestPendingPod("pod-3", "test-ns", nil)
	podTerminating := createTestTerminatingPod("pod-4", "test-ns", nil)

	// Case 1: 2 pods running, within [1, 3] -> acceptable
	status2 := measurementutil.ComputePodsStartupStatus([]*corev1.Pod{podRunning1, podRunning2}, 2, nil)
	if !isPodsStatusAcceptable([]*corev1.Pod{podRunning1, podRunning2}, status2, 1, 3) {
		t.Errorf("expected status to be acceptable when running pods is within range")
	}

	// Case 2: 1 pod running, 1 pod pending -> not acceptable
	statusPending := measurementutil.ComputePodsStartupStatus([]*corev1.Pod{podRunning1, podPending}, 2, nil)
	if isPodsStatusAcceptable([]*corev1.Pod{podRunning1, podPending}, statusPending, 1, 3) {
		t.Errorf("expected status to NOT be acceptable when a pod is pending")
	}

	// Case 3: 0 pods running, min is 1 -> not acceptable
	status0 := measurementutil.ComputePodsStartupStatus([]*corev1.Pod{}, 0, nil)
	if isPodsStatusAcceptable([]*corev1.Pod{}, status0, 1, 3) {
		t.Errorf("expected status to NOT be acceptable when running pods (0) < min (1)")
	}

	// Case 4: 1 pod running, 1 pod terminating -> not acceptable
	statusTerminating := measurementutil.ComputePodsStartupStatus([]*corev1.Pod{podRunning1, podTerminating}, 2, nil)
	if isPodsStatusAcceptable([]*corev1.Pod{podRunning1, podTerminating}, statusTerminating, 1, 3) {
		t.Errorf("expected status to NOT be acceptable when a pod is terminating")
	}
}

func TestGetNotRunningPods(t *testing.T) {
	pods := []*corev1.Pod{
		createTestRunningPod("pod-running", "default", nil),
		createTestPendingPod("pod-pending", "kube-system", nil),
		createTestTerminatingPod("pod-terminating", "custom-ns", nil),
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-not-ready",
				Namespace: "test-ns",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
		},
	}

	notRunning := getNotRunningPods(pods)
	expected := []string{
		"kube-system/pod-pending",
		"custom-ns/pod-terminating",
		"test-ns/pod-not-ready",
	}

	if len(notRunning) != len(expected) {
		t.Fatalf("expected %d not running pods, got %d: %v", len(expected), len(notRunning), notRunning)
	}
	for i, exp := range expected {
		if notRunning[i] != exp {
			t.Errorf("notRunning[%d] = %s, want %s", i, notRunning[i], exp)
		}
	}
}

func TestWaitForPods_TimeoutListsNotRunningPods(t *testing.T) {
	pods := []*corev1.Pod{
		createTestRunningPod("pod-1", "test-ns", nil),
		createTestPendingPod("pod-2", "test-ns", nil),
	}

	lister := &fakePodLister{pods: pods}
	w := &waitForRunningPodsRestartMeasurement{
		callerName: "TestTimeout",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := w.waitForPods(ctx, lister, 2, 2, 2, 10*time.Millisecond, 0)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "test-ns/pod-2") {
		t.Errorf("expected error message to contain not running pod 'test-ns/pod-2', got: %v", err)
	}
}
