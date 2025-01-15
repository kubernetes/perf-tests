package common

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func TestJobLifecycleLatencyMeasurement(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := createJobLifecycleLatencyMeasurement().(*jobLifecycleLatencyMeasurement)
	m.eventTicker.Reset(time.Second)

	startConfig := &measurement.Config{
		Params: map[string]interface{}{
			"action": "start",
		},
		ClusterFramework: framework.NewFakeFramework(client),
	}
	if _, err := m.Execute(startConfig); err != nil {
		t.Fatalf("Failed to start measurement: %v", err)
	}

	start := time.Now()
	namespace := "default"
	jobs := []batchv1.Job{
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:              "job0",
				Namespace:         namespace,
				CreationTimestamp: metav1.Time{Time: start},
			},
			Status: batchv1.JobStatus{
				StartTime:      &metav1.Time{Time: start.Add(1 * time.Minute)},
				CompletionTime: &metav1.Time{Time: start.Add(2 * time.Minute)},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:              "job1",
				Namespace:         namespace,
				CreationTimestamp: metav1.Time{Time: start.Add(30 * time.Second)},
			},
			Status: batchv1.JobStatus{
				StartTime:      &metav1.Time{Time: start.Add(2 * time.Minute)},
				CompletionTime: &metav1.Time{Time: start.Add(4 * time.Minute)},
			},
		},
	}
	for _, job := range jobs {
		if _, err := client.BatchV1().Jobs(namespace).Create(context.TODO(), &job, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}

	events := []corev1.Event{
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Event",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "event0",
			},
			InvolvedObject: corev1.ObjectReference{
				Name:      "job0",
				Namespace: namespace,
			},
			Reason:         "SuccessfulCreate",
			Message:        "Created pod: pod0",
			FirstTimestamp: metav1.Time{Time: start.Add(2 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(2 * time.Minute)},
			Count:          1,
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Event",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "event1",
			},
			InvolvedObject: corev1.ObjectReference{
				Name:      "job1",
				Namespace: namespace,
			},
			Reason:         "SuccessfulCreate",
			Message:        "Created pod: pod1",
			FirstTimestamp: metav1.Time{Time: start.Add(5 * time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(5 * time.Minute)},
			Count:          1,
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Event",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "event2",
			},
			InvolvedObject: corev1.ObjectReference{
				Name:      "job1",
				Namespace: namespace,
			},
			Reason:         "SuccessfulCreate",
			Message:        "(combined from similar events): Created pod: pod2",
			FirstTimestamp: metav1.Time{Time: start.Add(time.Minute)},
			LastTimestamp:  metav1.Time{Time: start.Add(6 * time.Minute)},
			Count:          11,
		},
	}
	for _, event := range events {
		_, err := client.CoreV1().Events(namespace).Create(context.TODO(), &event, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create event: %v", err)
		}
	}

	time.Sleep(2 * time.Second)

	gatherConfig := &measurement.Config{
		Identifier: "test",
		Params: map[string]interface{}{
			"action": "gather",
		},
		ClusterFramework: framework.NewFakeFramework(client),
	}
	summaries, err := m.Execute(gatherConfig)
	if err != nil {
		t.Fatalf("Failed to gather measurement: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("Expect 1 summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.SummaryName() != "JobLifecycleLatency_test" {
		t.Errorf("Unexpected summary name: %s", summary.SummaryName())
	}
	var perfData map[string]interface{}
	if err := json.Unmarshal([]byte(summary.SummaryContent()), &perfData); err != nil {
		t.Fatalf("Failed to parse summary content as JSON: %v", err)
	}

	{
		createToPodStart := findDataItem(perfData, "create_to_pod_start")
		if createToPodStart == nil {
			t.Fatalf("Missing create_to_pod_start in summary")
		}
		gotP50 := getMetric(createToPodStart, "Perc50")
		if gotP50 != 3*time.Minute {
			t.Errorf("Expect create_to_pod_start Perc50 = 3 minutes, got %v", gotP50)
		}
		gotP90 := getMetric(createToPodStart, "Perc90")
		if gotP90 != 5*time.Minute {
			t.Errorf("Expect create_to_pod_start Perc90 = 5 minutes, got %v", gotP90)
		}
		gotP99 := getMetric(createToPodStart, "Perc99")
		if gotP99 != 5*time.Minute+30*time.Second {
			t.Errorf("Expect create_to_pod_start Perc99 = 5.5 minutes, got %v", gotP99)
		}
	}
	{
		createToStart := findDataItem(perfData, "create_to_start")
		if createToStart == nil {
			t.Fatalf("Missing create_to_start in summary")
		}
		gotP50 := getMetric(createToStart, "Perc50")
		if gotP50 != time.Minute {
			t.Errorf("Expect create_to_start Perc50 = 1 minute, got %v", gotP50)
		}
		gotP90 := getMetric(createToStart, "Perc90")
		if gotP90 != time.Minute+30*time.Second {
			t.Errorf("Expect create_to_start Perc90 = 1.5 minutes, got %v", gotP90)
		}
		gotP99 := getMetric(createToStart, "Perc99")
		if gotP99 != time.Minute+30*time.Second {
			t.Errorf("Expect create_to_start Perc99 = 1.5 minutes, got %v", gotP99)
		}
	}
	{
		startToComplete := findDataItem(perfData, "start_to_complete")
		if startToComplete == nil {
			t.Fatalf("Missing start_to_complete in summary")
		}
		gotP50 := getMetric(startToComplete, "Perc50")
		if gotP50 != time.Minute {
			t.Errorf("Expect start_to_complete Perc50 = 1 minutes, got %v", gotP50)
		}
		gotP90 := getMetric(startToComplete, "Perc90")
		if gotP90 != 2*time.Minute {
			t.Errorf("Expect start_to_complete Perc90 = 2 minutes, got %v", gotP90)
		}
		gotP99 := getMetric(startToComplete, "Perc99")
		if gotP99 != 2*time.Minute {
			t.Errorf("Expect start_to_complete Perc99 = 2 minutes, got %v", gotP99)
		}
	}
}

func findDataItem(perfData map[string]interface{}, name string) map[string]interface{} {
	dataItems := perfData["dataItems"].([]interface{})
	for _, item := range dataItems {
		casted := item.(map[string]interface{})
		labels := casted["labels"].(map[string]interface{})
		if labels["Metric"] == name {
			return casted
		}
	}
	return nil
}

func getMetric(dataItem map[string]interface{}, metric string) time.Duration {
	return time.Duration(dataItem["data"].(map[string]interface{})[metric].(float64)) * time.Millisecond
}
