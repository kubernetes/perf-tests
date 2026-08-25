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

package dnspropagation

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetPodReadyTransitionTime(t *testing.T) {
	now := metav1.Now()

	testCases := []struct {
		name       string
		pod        *v1.Pod
		wantTime   time.Time
		wantIsRead bool
	}{
		{
			name: "pod with no conditions",
			pod: &v1.Pod{
				Status: v1.PodStatus{},
			},
			wantTime:   time.Time{},
			wantIsRead: false,
		},
		{
			name: "pod with PodReady = False",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:               v1.PodReady,
							Status:             v1.ConditionFalse,
							LastTransitionTime: now,
						},
					},
				},
			},
			wantTime:   time.Time{},
			wantIsRead: false,
		},
		{
			name: "pod with unrelated condition = True",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:               v1.PodScheduled,
							Status:             v1.ConditionTrue,
							LastTransitionTime: now,
						},
					},
				},
			},
			wantTime:   time.Time{},
			wantIsRead: false,
		},
		{
			name: "pod with PodReady = True",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:               v1.PodReady,
							Status:             v1.ConditionTrue,
							LastTransitionTime: now,
						},
					},
				},
			},
			wantTime:   now.Time,
			wantIsRead: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotTime, gotIsReady := getPodReadyTransitionTime(tc.pod)
			if gotIsReady != tc.wantIsRead {
				t.Errorf("getPodReadyTransitionTime() gotIsReady = %v, want %v", gotIsReady, tc.wantIsRead)
			}
			if !gotTime.Equal(tc.wantTime) {
				t.Errorf("getPodReadyTransitionTime() gotTime = %v, want %v", gotTime, tc.wantTime)
			}
		})
	}
}

func TestSelectSample(t *testing.T) {
	podTotal := 50
	sampleTotal := 10

	samples := selectSample(podTotal, sampleTotal)
	if len(samples) != sampleTotal {
		t.Fatalf("selectSample() returned %d samples, want %d", len(samples), sampleTotal)
	}

	seen := make(map[int]bool)
	for _, idx := range samples {
		if idx < 0 || idx >= podTotal {
			t.Errorf("sample index %d out of bounds [0, %d)", idx, podTotal)
		}
		if seen[idx] {
			t.Errorf("duplicate sample index %d selected", idx)
		}
		seen[idx] = true
	}
}

func TestProbeDNSUntilResolved(t *testing.T) {
	readyTime := time.Now().Add(-500 * time.Millisecond)
	var attempts atomic.Int32

	oldLookup := lookupFunc
	defer func() { lookupFunc = oldLookup }()

	lookupFunc = func(url string) error {
		if attempts.Add(1) < 3 {
			return errors.New("NXDOMAIN")
		}
		return nil
	}

	duration := probeDNSUntilResolved("test-pod.test-svc.default.svc.cluster.local", readyTime, 10*time.Millisecond)
	if duration <= 0 {
		t.Errorf("probeDNSUntilResolved() returned non-positive duration: %v", duration)
	}

	if attempts.Load() < 3 {
		t.Errorf("lookupFunc was called %d times, expected at least 3", attempts.Load())
	}
}

func TestTransformPod(t *testing.T) {
	now := metav1.Now()
	fullPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod-0",
			Namespace:       "test-ns",
			Annotations:     map[string]string{"heavy": "annotation"},
			Labels:          map[string]string{"heavy": "label"},
			ResourceVersion: "12345",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "container-1",
					Image: "heavy-image:v1",
				},
			},
		},
		Status: v1.PodStatus{
			Conditions: []v1.PodCondition{
				{
					Type:               v1.PodReady,
					Status:             v1.ConditionTrue,
					LastTransitionTime: now,
				},
			},
		},
	}

	transformed, err := transformPod(fullPod)
	if err != nil {
		t.Fatalf("transformPod returned error: %v", err)
	}

	strippedPod, ok := transformed.(*v1.Pod)
	if !ok {
		t.Fatalf("transformPod did not return *v1.Pod")
	}

	if strippedPod.Name != "test-pod-0" || strippedPod.Namespace != "test-ns" {
		t.Errorf("transformPod did not preserve Name and Namespace")
	}

	if len(strippedPod.Spec.Containers) != 0 {
		t.Errorf("transformPod did not strip Spec.Containers")
	}

	if len(strippedPod.Annotations) != 0 {
		t.Errorf("transformPod did not strip Annotations")
	}

	if len(strippedPod.Status.Conditions) != 1 || strippedPod.Status.Conditions[0].Type != v1.PodReady {
		t.Errorf("transformPod did not preserve Status.Conditions")
	}
}

func TestRunProbeWithInformer(t *testing.T) {
	stsName := "test-sts"
	svcName := "test-svc"
	nsName := "test-ns"
	count := 2
	samples := 2

	statefulSet = &stsName
	service = &svcName
	namespace = &nsName
	podCount = &count
	sampleCount = &samples
	intervalVal := 10 * time.Millisecond
	interval = &intervalVal

	oldLookup := lookupFunc
	defer func() { lookupFunc = oldLookup }()
	lookupFunc = func(url string) error {
		return nil
	}

	fakeWatcher := watch.NewFake()
	client := fake.NewClientset()
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, fakeWatcher, nil
	})

	now := metav1.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		for i := 0; i < count; i++ {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d", stsName, i),
					Namespace: nsName,
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:               v1.PodReady,
							Status:             v1.ConditionTrue,
							LastTransitionTime: now,
						},
					},
				},
			}
			fakeWatcher.Add(pod)
		}
	}()

	done := make(chan struct{})
	go func() {
		runProbe(client)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runProbe timed out waiting for informer events and DNS probes to complete")
	}
}
