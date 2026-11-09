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

package dnspropagation

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func endpoint(kind, name string, ready *bool) discoveryv1.Endpoint {
	e := discoveryv1.Endpoint{
		Addresses:  []string{"10.0.0.1"},
		Conditions: discoveryv1.EndpointConditions{Ready: ready},
	}
	if kind != "" {
		e.TargetRef = &v1.ObjectReference{Kind: kind, Name: name, Namespace: "ns"}
	}
	return e
}

func TestReadyEndpointPods(t *testing.T) {
	yes, no := true, false
	slice := &discoveryv1.EndpointSlice{
		Endpoints: []discoveryv1.Endpoint{
			endpoint("Pod", "ready", &yes),
			endpoint("Pod", "not-ready", &no),
			endpoint("Pod", "unknown", nil),
			endpoint("Node", "node", &yes),
			endpoint("", "", &yes),
			endpoint("Pod", "ready-2", &yes),
		},
	}

	got := readyEndpointPods(slice)

	want := []string{"ready", "ready-2"}
	if !slices.Equal(got, want) {
		t.Errorf("readyEndpointPods() = %v, want %v", got, want)
	}
}

func TestTransformEndpointSlice(t *testing.T) {
	yes := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-abc",
			Namespace: "ns",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "svc"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{endpoint("Pod", "pod-0", &yes), endpoint("", "", &yes)},
	}

	obj, err := transformEndpointSlice(slice)
	if err != nil {
		t.Fatalf("transformEndpointSlice() error: %v", err)
	}

	stripped := obj.(*discoveryv1.EndpointSlice)
	if stripped.Name != "svc-abc" || stripped.Namespace != "ns" || len(stripped.Labels) != 0 {
		t.Errorf("transformEndpointSlice() metadata = %+v", stripped.ObjectMeta)
	}
	if got := readyEndpointPods(stripped); !slices.Equal(got, []string{"pod-0"}) {
		t.Errorf("readyEndpointPods(transformed) = %v, want [pod-0]", got)
	}
	if len(stripped.Endpoints[0].Addresses) != 0 || stripped.Endpoints[0].TargetRef.Namespace != "" {
		t.Errorf("transformEndpointSlice() kept unneeded fields: %+v", stripped.Endpoints[0])
	}
}

func gaugeValue(t *testing.T, gauge *prometheus.GaugeVec, pod string) (float64, bool) {
	t.Helper()
	labels := prometheus.Labels{"namespace": *namespace, "service": *service, "podName": pod}
	g, err := gauge.GetMetricWith(labels)
	if err != nil {
		t.Fatalf("GetMetricWith(%v): %v", labels, err)
	}
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	// GetMetricWith creates missing series with value 0, so delete it again to keep the
	// "not set" state observable.
	if m.GetGauge().GetValue() == 0 {
		gauge.Delete(labels)
		return 0, false
	}
	return m.GetGauge().GetValue(), true
}

func TestStageTracker(t *testing.T) {
	ns, svc := "stage-ns", "stage-svc"
	namespace, service = &ns, &svc
	ready := time.Date(2026, 11, 7, 2, 30, 0, 0, time.UTC)
	tracker := newStageTracker(map[string]struct{}{"pod-0": {}})

	tracker.endpointReady("pod-0", ready.Add(3*time.Second))
	if _, ok := gaugeValue(t, DNSPropagationEndpointSeconds, "pod-0"); ok {
		t.Errorf("endpoint stage set before the Ready time is known")
	}
	tracker.podSeen("pod-0", ready, ready.Add(2*time.Second))
	tracker.resolved("pod-0", ready.Add(5*time.Second))

	// First observations win.
	tracker.podSeen("pod-0", ready, ready.Add(9*time.Second))
	tracker.endpointReady("pod-0", ready.Add(9*time.Second))
	tracker.resolved("pod-0", ready.Add(9*time.Second))
	// Pods outside the sample are ignored.
	tracker.endpointReady("pod-1", ready)

	for _, tc := range []struct {
		name  string
		gauge *prometheus.GaugeVec
		want  float64
	}{
		{"pod seen", DNSPropagationPodSeenSeconds, 2},
		{"endpoint", DNSPropagationEndpointSeconds, 3},
		{"after endpoint", DNSPropagationAfterEndpointSeconds, 2},
		{"lookup", DNSPropagationLookupSeconds, 3},
	} {
		got, ok := gaugeValue(t, tc.gauge, "pod-0")
		if !ok || got != tc.want {
			t.Errorf("%s stage = %v (set %v), want %v", tc.name, got, ok, tc.want)
		}
	}
	if _, ok := gaugeValue(t, DNSPropagationEndpointSeconds, "pod-1"); ok {
		t.Errorf("endpoint stage set for a pod outside the sample")
	}
}

func TestRunProbeRecordsEndpoints(t *testing.T) {
	stsName, svcName, nsName := "endpoint-sts", "endpoint-svc", "endpoint-ns"
	count, samples := 2, 2
	statefulSet, service, namespace = &stsName, &svcName, &nsName
	podCount, sampleCount = &count, &samples
	intervalVal := 10 * time.Millisecond
	interval = &intervalVal

	oldLookup := lookupFunc
	defer func() { lookupFunc = oldLookup }()
	lookupFunc = func(url string) error { return nil }

	podWatcher, sliceWatcher := watch.NewFake(), watch.NewFake()
	client := fake.NewClientset()
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, podWatcher, nil
	})
	client.PrependWatchReactor("endpointslices", func(action k8stesting.Action) (bool, watch.Interface, error) {
		return true, sliceWatcher, nil
	})

	ready := metav1.NewTime(time.Now().Add(-time.Second).Truncate(time.Second))
	yes := true
	go func() {
		time.Sleep(20 * time.Millisecond)
		slice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName + "-abc",
				Namespace: nsName,
				Labels:    map[string]string{discoveryv1.LabelServiceName: svcName},
			},
		}
		for i := 0; i < count; i++ {
			slice.Endpoints = append(slice.Endpoints, endpoint("Pod", fmt.Sprintf("%s-%d", stsName, i), &yes))
		}
		sliceWatcher.Add(slice)
		for i := 0; i < count; i++ {
			podWatcher.Add(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", stsName, i), Namespace: nsName},
				Status: v1.PodStatus{Conditions: []v1.PodCondition{
					{Type: v1.PodReady, Status: v1.ConditionTrue, LastTransitionTime: ready},
				}},
			})
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
		t.Fatal("runProbe timed out")
	}

	deadline := time.Now().Add(5 * time.Second)
	for i := 0; i < count; i++ {
		pod := fmt.Sprintf("%s-%d", stsName, i)
		for {
			if v, ok := gaugeValue(t, DNSPropagationEndpointSeconds, pod); ok && v > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("endpoint stage of %s was not recorded", pod)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if v, ok := gaugeValue(t, DNSPropagationLookupSeconds, pod); !ok || v <= 0 {
			t.Errorf("lookup stage of %s = %v (set %v), want > 0", pod, v, ok)
		}
	}
}
