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
