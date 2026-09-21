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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	clocktesting "k8s.io/utils/clock/testing"
)

func TestPodStartupLatency(t *testing.T) {
	p := createPodStartupLatencyMeasurement().(*podStartupLatencyMeasurement)
	t0 := time.Unix(100, 0)
	fc := clocktesting.NewFakeClock(t0)
	p.clock = fc
	p.selector.Namespace = "default"
	p.isRunning = true
	p.stopCh = make(chan struct{})
	p.threshold = 10 * time.Second
	p.perc50Threshold = 10 * time.Second
	p.perc90Threshold = 10 * time.Second
	p.perc99Threshold = 10 * time.Second

	// We will have 2 pods:
	// pod-stateless:
	//   created: t0
	//   scheduled: t0 + 2s
	//   running: t0 + 5s
	//   latencies:
	//     create_to_schedule: 2s (2000 ms)
	//     schedule_to_run: 3s (3000 ms)
	//     pod_startup: 5s (5000 ms)
	// pod-stateful:
	//   created: t0 + 10s
	//   scheduled: t0 + 12s
	//   running: t0 + 18s
	//   latencies:
	//     create_to_schedule: 2s (2000 ms)
	//     schedule_to_run: 6s (6000 ms)
	//     pod_startup: 8s (8000 ms) (This starts within 10s threshold to avoid SLO violation logspam)

	// Event series:
	// 1. Stateless pod created
	podStateless := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-stateless",
			Namespace: "default",
		},
	}
	p.addEvent(nil, podStateless)

	// 2. Stateless pod scheduled at t0 + 2s
	fc.SetTime(t0.Add(2 * time.Second))
	podStatelessScheduled := podStateless.DeepCopy()
	podStatelessScheduled.Spec.NodeName = "node1"
	p.addEvent(nil, podStatelessScheduled)

	// 3. Stateless pod running at t0 + 5s
	fc.SetTime(t0.Add(5 * time.Second))
	podStatelessRunning := podStatelessScheduled.DeepCopy()
	podStatelessRunning.Status.Phase = corev1.PodRunning
	p.addEvent(nil, podStatelessRunning)

	// 4. Stateful pod created at t0 + 10s
	fc.SetTime(t0.Add(10 * time.Second))
	podStateful := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-stateful",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "stateful-vol",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/data"},
					},
				},
			},
		},
	}
	p.addEvent(nil, podStateful)

	// 5. Stateful pod scheduled at t0 + 12s
	fc.SetTime(t0.Add(12 * time.Second))
	podStatefulScheduled := podStateful.DeepCopy()
	podStatefulScheduled.Spec.NodeName = "node1"
	p.addEvent(nil, podStatefulScheduled)

	// 6. Stateful pod running at t0 + 18s
	fc.SetTime(t0.Add(18 * time.Second))
	podStatefulRunning := podStatefulScheduled.DeepCopy()
	podStatefulRunning.Status.Phase = corev1.PodRunning
	p.addEvent(nil, podStatefulRunning)

	// Process all work items
	for p.eventQueue.Len() > 0 {
		ok := p.processNextWorkItem()
		if !ok {
			t.Fatalf("Failed to process next work item")
		}
	}

	// Gather the results
	summaries, err := p.gather(nil, "test-run", "")
	if err != nil {
		t.Fatalf("Gather failed unexpectedly: %v", err)
	}

	// We expect 3 summaries:
	// 1. stateless & stateful combined (namePrefix: "")
	// 2. stateless (namePrefix: "Stateless")
	// 3. stateful (namePrefix: "Stateful")
	if len(summaries) != 3 {
		t.Fatalf("Expected 3 summaries, got %d", len(summaries))
	}

	summaryMap := make(map[string]string)
	for _, s := range summaries {
		summaryMap[s.SummaryName()] = s.SummaryContent()
	}

	assert.Contains(t, summaryMap, "PodStartupLatency_test-run")
	assert.Contains(t, summaryMap, "StatelessPodStartupLatency_test-run")
	assert.Contains(t, summaryMap, "StatefulPodStartupLatency_test-run")

	// Verify stateless pod latencies
	var statelessData measurementutil.PerfData
	err = json.Unmarshal([]byte(summaryMap["StatelessPodStartupLatency_test-run"]), &statelessData)
	if err != nil {
		t.Fatalf("Failed to unmarshal stateless summary: %v", err)
	}

	// For a single data point (percentiles 50, 90, 99 should be equal to that data point)
	// create_to_schedule = 2s (2000ms)
	// schedule_to_run = 3s (3000ms)
	// pod_startup = 5s (5000ms)
	for _, item := range statelessData.DataItems {
		metric := item.Labels["Metric"]
		switch metric {
		case "create_to_schedule":
			assert.Equal(t, 2000.0, item.Data["Perc50"])
		case "schedule_to_run":
			assert.Equal(t, 3000.0, item.Data["Perc50"])
		case "pod_startup":
			assert.Equal(t, 5000.0, item.Data["Perc50"])
		}
	}

	// Verify stateful pod latencies
	var statefulData measurementutil.PerfData
	err = json.Unmarshal([]byte(summaryMap["StatefulPodStartupLatency_test-run"]), &statefulData)
	if err != nil {
		t.Fatalf("Failed to unmarshal stateful summary: %v", err)
	}

	// create_to_schedule = 2s (2000ms)
	// schedule_to_run = 6s (6000ms)
	// pod_startup = 8s (8000ms)
	for _, item := range statefulData.DataItems {
		metric := item.Labels["Metric"]
		switch metric {
		case "create_to_schedule":
			assert.Equal(t, 2000.0, item.Data["Perc50"])
		case "schedule_to_run":
			assert.Equal(t, 6000.0, item.Data["Perc50"])
		case "pod_startup":
			assert.Equal(t, 8000.0, item.Data["Perc50"])
		}
	}
}
