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

package slos

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	clocktesting "k8s.io/utils/clock/testing"
)

// setupMeasurement creates a resourceClaimAllocationLatencyMeasurement with a
// FakeClock injected, bypassing start() so no real cluster is required.
func setupMeasurement(t *testing.T) (*resourceClaimAllocationLatencyMeasurement, *clocktesting.FakeClock, time.Time) {
	t.Helper()
	m := createResourceClaimAllocationLatencyMeasurement().(*resourceClaimAllocationLatencyMeasurement)
	t0 := time.Unix(100, 0)
	fc := clocktesting.NewFakeClock(t0)
	m.clock = fc
	m.isRunning = true
	m.stopCh = make(chan struct{})
	// Set thresholds high enough to avoid SLO violation errors during gather.
	m.threshold = time.Minute
	m.perc50Threshold = time.Minute
	m.perc90Threshold = time.Minute
	m.perc99Threshold = time.Minute
	return m, fc, t0
}

// drainQueue processes all pending work items synchronously.
func drainQueue(t *testing.T, m *resourceClaimAllocationLatencyMeasurement) {
	t.Helper()
	for m.queue.Len() > 0 {
		if !m.processNextWorkItem() {
			t.Fatal("processNextWorkItem returned false while queue was non-empty")
		}
	}
}

// makePod builds a minimal Pod that references a ResourceClaimTemplate.
// CreationTimestamp is set to second granularity (whole seconds) to demonstrate
// that the fix does not rely on it.
func makePod(name, namespace string, t0 time.Time) *corev1.Pod {
	tmplName := "gpu-claim-template"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			UID:               types.UID("pod-uid-" + name),
			CreationTimestamp: metav1.NewTime(t0), // second granularity — the bug source
		},
		Spec: corev1.PodSpec{
			ResourceClaims: []corev1.PodResourceClaim{
				{Name: "gpu", ResourceClaimTemplateName: &tmplName},
			},
		},
	}
}

// makeClaim builds a ResourceClaim owned by the given pod.
// CreationTimestamp is set to second granularity (whole seconds).
func makeClaim(name, namespace, podName, podUID string, t0 time.Time, allocated bool) *resourcev1.ResourceClaim {
	cl := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(t0), // second granularity — the bug source
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Pod", Name: podName, APIVersion: "v1", UID: types.UID(podUID)},
			},
		},
	}
	if allocated {
		cl.Status = resourcev1.ResourceClaimStatus{
			Devices: []resourcev1.AllocatedDeviceStatus{
				{Driver: "test.driver.io"},
			},
		}
	}
	return cl
}

// TestResourceClaimAllocationLatency verifies that claim_allocation uses watch-stream
// receive times (nanosecond precision) for both endpoints, not CreationTimestamp.
//
// Timeline:
//
//	t0 + 250ms: Pod observed via addPodEvent      -> podCreationTimes["default/pod1"] = t0+250ms
//	t0 + 750ms: unallocated claim via addEvent    -> createPhase = t0+750ms
//	t0 +1250ms: allocated claim via addEvent      -> allocatePhase = t0+1250ms
//
// Expected:
//
//	claim_allocation        = 500ms
//	pod_create_to_claim_create = 500ms
//
// Buggy code produces claim_allocation = 1250ms (createPhase = t0 from CreationTimestamp).
func TestResourceClaimAllocationLatency(t *testing.T) {
	m, fc, t0 := setupMeasurement(t)

	// t0 + 250ms: pod is observed.
	fc.SetTime(t0.Add(250 * time.Millisecond))
	pod := makePod("pod1", "default", t0)
	m.addPodEvent(nil, pod)

	// t0 + 750ms: unallocated ResourceClaim is first observed.
	fc.SetTime(t0.Add(750 * time.Millisecond))
	claim := makeClaim("pod1-gpu", "default", "pod1", "pod-uid-pod1", t0, false)
	m.addEvent(nil, claim)

	// t0 + 1250ms: same ResourceClaim is now observed as allocated.
	fc.SetTime(t0.Add(1250 * time.Millisecond))
	allocatedClaim := claim.DeepCopy()
	allocatedClaim.Status = resourcev1.ResourceClaimStatus{
		Devices: []resourcev1.AllocatedDeviceStatus{{Driver: "test.driver.io"}},
	}
	m.addEvent(nil, allocatedClaim)

	drainQueue(t, m)

	summaries, err := m.gather(nil, "test-run")
	if err != nil {
		t.Fatalf("gather failed unexpectedly: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	var perf measurementutil.PerfData
	if err := json.Unmarshal([]byte(summaries[0].SummaryContent()), &perf); err != nil {
		t.Fatalf("failed to unmarshal summary: %v", err)
	}

	foundAlloc, foundPodToClaim := false, false
	for _, item := range perf.DataItems {
		switch item.Labels["Metric"] {
		case "claim_allocation":
			// Fix: createPhase = t0+750ms, allocatePhase = t0+1250ms → 500ms.
			// Bug: createPhase = t0 (CreationTimestamp), allocatePhase = t0+1250ms → 1250ms.
			assert.Equal(t, 500.0, item.Data["Perc50"], "claim_allocation Perc50 mismatch")
			foundAlloc = true
		case "pod_create_to_claim_create":
			// Fix: podCreatePhase = t0+250ms, createPhase = t0+750ms → 500ms.
			// Bug: podCreatePhase = t0 (CreationTimestamp), createPhase = t0+750ms → 750ms.
			assert.Equal(t, 500.0, item.Data["Perc50"], "pod_create_to_claim_create Perc50 mismatch")
			foundPodToClaim = true
		}
	}
	if !foundAlloc {
		t.Error("claim_allocation metric not found in summary")
	}
	if !foundPodToClaim {
		t.Error("pod_create_to_claim_create metric not found in summary")
	}
}

// TestResourceClaimPodToClaimLatency verifies that pod_create_to_claim_create uses
// the watch-stream receive time for the pod (nanosecond precision), not CreationTimestamp.
//
// Timeline:
//
//	t0 + 200ms: Pod observed via addPodEvent      -> podCreationTimes["default/pod2"] = t0+200ms
//	t0 + 900ms: unallocated claim via addEvent    -> createPhase = t0+900ms
//
// Expected:
//
//	pod_create_to_claim_create = 700ms
//
// Buggy code produces 900ms (podCreatePhase = t0 from pod.CreationTimestamp.Time).
func TestResourceClaimPodToClaimLatency(t *testing.T) {
	m, fc, t0 := setupMeasurement(t)

	// t0 + 200ms: pod is observed.
	fc.SetTime(t0.Add(200 * time.Millisecond))
	pod := makePod("pod2", "default", t0)
	m.addPodEvent(nil, pod)

	// t0 + 900ms: unallocated claim is observed (no allocation in this test).
	fc.SetTime(t0.Add(900 * time.Millisecond))
	claim := makeClaim("pod2-gpu", "default", "pod2", "pod-uid-pod2", t0, false)
	m.addEvent(nil, claim)

	drainQueue(t, m)

	// gather: claim_allocation has no data (no allocatePhase set) → Perc50=0 < 1min → no error.
	summaries, err := m.gather(nil, "test-run")
	if err != nil {
		t.Fatalf("gather failed unexpectedly: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	var perf measurementutil.PerfData
	if err := json.Unmarshal([]byte(summaries[0].SummaryContent()), &perf); err != nil {
		t.Fatalf("failed to unmarshal summary: %v", err)
	}

	found := false
	for _, item := range perf.DataItems {
		if item.Labels["Metric"] == "pod_create_to_claim_create" {
			// Fix: podCreatePhase = t0+200ms, createPhase = t0+900ms → 700ms.
			// Bug: podCreatePhase = t0 (CreationTimestamp), createPhase = t0+900ms → 900ms.
			assert.Equal(t, 700.0, item.Data["Perc50"], "pod_create_to_claim_create Perc50 mismatch")
			found = true
		}
	}
	if !found {
		t.Error("pod_create_to_claim_create metric not found in summary")
	}
}

// TestResourceClaimAlreadyAllocatedIgnored verifies that the existing guard correctly
// ignores a ResourceClaim that arrives already-allocated with no prior createPhase entry.
// This simulates a claim that was allocated before the measurement started (e.g., from
// the informer's initial LIST phase).
func TestResourceClaimAlreadyAllocatedIgnored(t *testing.T) {
	m, fc, t0 := setupMeasurement(t)

	// A claim that is already allocated on first observation (simulates LIST behavior).
	fc.SetTime(t0.Add(500 * time.Millisecond))
	allocatedClaim := makeClaim("pre-alloc-gpu", "default", "pod3", "pod-uid-pod3", t0, true)
	m.addEvent(nil, allocatedClaim)

	drainQueue(t, m)

	// The guard (processEvent lines: "if allocated && !found createPhase -> return")
	// must have fired. No createPhase entry should have been recorded.
	assert.Equal(t, 0, m.entries.Count(createPhase),
		"createPhase must not be recorded for a pre-existing allocated claim")
	assert.Equal(t, 0, m.entries.Count(allocatePhase),
		"allocatePhase must not be recorded for a pre-existing allocated claim")
}
