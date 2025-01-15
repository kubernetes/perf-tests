package util

import (
	"fmt"
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectTransitionTimesIterator(t *testing.T) {
	ott := NewObjectTransitionTimes("test")
	ott.Set("obj0", "phase1", time.Now())
	ott.Set("obj1", "phase1", time.Now())
	ott.Set("obj2", "phase1", time.Now())

	var objects []string
	for obj := range ott.Keys() {
		objects = append(objects, obj)
	}

	if len(objects) != 3 {
		t.Errorf("expect 3 objects, got %d", len(objects))
	}

	sort.Strings(objects)
	for i := 0; i < 3; i++ {
		expected := fmt.Sprintf("obj%d", i)
		if objects[i] != expected {
			t.Errorf("expect %q at index %d, got %q", expected, i, objects[i])
		}
	}
}

func TestPodCreationEventTimes(t *testing.T) {
	start := time.Now()
	pcet := NewPodCreationEventTimes()
	pcet.Set(
		"default/job0",
		&corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Name: "job0",
			},
			Message:        "Created pod: pod0",
			FirstTimestamp: metav1.Time{Time: start.Add(2 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(2 * time.Minute)},
			Count:          1,
		})
	pcet.Set(
		"default/job1",
		&corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Name: "job1",
			},
			Message:        "Created pod: pod1",
			FirstTimestamp: metav1.Time{Time: start.Add(5 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(5 * time.Minute)},
			Count:          1,
		})
	pcet.Set(
		"default/job1",
		&corev1.Event{
			InvolvedObject: corev1.ObjectReference{
				Name: "job1",
			},
			Message:        "(combined from similar events): Created pod: pod2",
			FirstTimestamp: metav1.Time{Time: start.Add(time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(6 * time.Minute)},
			Count:          11,
		})

	jobCreationTimes := map[string]time.Time{
		"default/job0": start,
		"default/job1": start.Add(30 * time.Second),
	}
	latencyMetric := pcet.CalculateLatency(jobCreationTimes)
	if latencyMetric.Perc50 != 3*time.Minute {
		t.Errorf("expected Perc50 = 3 minutes, got %v", latencyMetric.Perc50)
	}
	if latencyMetric.Perc90 != 5*time.Minute {
		t.Errorf("expected Perc90 = 5 minutes, got %v", latencyMetric.Perc90)
	}
	if latencyMetric.Perc99 != 5*time.Minute+30*time.Second {
		t.Errorf("expected Perc99 = 5.5 minutes, got %v", latencyMetric.Perc99)
	}
}
