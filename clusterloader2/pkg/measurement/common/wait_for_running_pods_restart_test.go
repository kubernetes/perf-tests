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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func createTestRunningPod(name, namespace string, podLabels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          podLabels,
			ResourceVersion: "1",
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

func createTestPendingPod(name, namespace string, podLabels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          podLabels,
			ResourceVersion: "1",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

func createTestTerminatingPod(name, namespace string, podLabels map[string]string) *corev1.Pod {
	now := metav1.Now()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            podLabels,
			DeletionTimestamp: &now,
			ResourceVersion:   "1",
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

func newFakePodClient(objs ...runtime.Object) *fake.Clientset {
	client := fake.NewSimpleClientset(objs...)
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listAction := action.(k8stesting.ListAction)
		gvk := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
		res, err := client.Tracker().List(action.GetResource(), gvk, listAction.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		if list, ok := res.(*corev1.PodList); ok {
			labelSelector := listAction.GetListRestrictions().Labels
			var filtered []corev1.Pod
			for _, pod := range list.Items {
				if labelSelector == nil || labelSelector.Matches(labels.Set(pod.Labels)) {
					filtered = append(filtered, pod)
				}
			}
			list.Items = filtered
			list.ListMeta.ResourceVersion = "1"
			return true, list, nil
		}
		return false, res, nil
	})
	client.PrependWatchReactor("pods", func(action k8stesting.Action) (bool, watch.Interface, error) {
		watchAction := action.(k8stesting.WatchAction)
		w, err := client.Tracker().Watch(action.GetResource(), watchAction.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		labelSelector := watchAction.GetWatchRestrictions().Labels
		if labelSelector == nil || labelSelector.Empty() {
			return true, w, nil
		}
		return true, watch.Filter(w, func(in watch.Event) (watch.Event, bool) {
			if pod, ok := in.Object.(*corev1.Pod); ok {
				return in, labelSelector.Matches(labels.Set(pod.Labels))
			}
			return in, true
		}), nil
	})
	return client
}

func TestWaitForRunningPodsRestart_Lifecycle(t *testing.T) {
	objects := []runtime.Object{
		createTestRunningPod("pod-1", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-2", "test-ns", map[string]string{"app": "foo"}),
		createTestPendingPod("pod-3", "test-ns", map[string]string{"app": "foo"}),
		createTestTerminatingPod("pod-4", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-other", "test-ns", map[string]string{"app": "bar"}),
	}

	fakeClient := newFakePodClient(objects...)
	multiClientSet := framework.NewMultiClientSetFromClients(fakeClient)
	clusterFramework := framework.NewFrameworkFromClients(multiClientSet, nil)

	m := createWaitForRunningPodsRestartMeasurement()

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

	// 2. Start should count all pods matching selector (4 pods: 2 running, 1 pending, 1 terminating)
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
	if measInstance.podsCount != 4 {
		t.Fatalf("expected 4 total pods counted on start, got %d", measInstance.podsCount)
	}

	// 3. Gather should succeed even though pod-3 is Pending and pod-4 is Terminating,
	// because WaitForPodsRecovery checks total pod count matching the selector.
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action":          "gather",
			"timeout":         "1s",
			"refreshInterval": "10ms",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error on gather: %v", err)
	}

	// 4. Stop should reset state
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

	// 5. Unknown action should error
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

func TestWaitForRunningPodsRestart_GatherWithTolerationAndRange(t *testing.T) {
	// Start with 4 pods matching selector
	objects := []runtime.Object{
		createTestRunningPod("pod-1", "test-ns", map[string]string{"app": "foo"}),
		createTestPendingPod("pod-2", "test-ns", map[string]string{"app": "foo"}),
		createTestPendingPod("pod-3", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-4", "test-ns", map[string]string{"app": "foo"}),
	}

	fakeClient := newFakePodClient(objects...)
	multiClientSet := framework.NewMultiClientSetFromClients(fakeClient)
	clusterFramework := framework.NewFrameworkFromClients(multiClientSet, nil)

	m := createWaitForRunningPodsRestartMeasurement()

	_, err := m.Execute(&measurement.Config{
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

	// Delete 1 pod so only 3 pods remain
	_ = fakeClient.Tracker().Delete(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "test-ns", "pod-4")

	// Gather with toleration 25% (4 ± ceil(4*0.25) = [3, 5]) should succeed with 3 pods
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action":          "gather",
			"toleration":      25.0,
			"timeout":         "1s",
			"refreshInterval": "10ms",
		},
	})
	if err != nil {
		t.Fatalf("expected gather with 25%% toleration to succeed when 3 of 4 pods exist, got: %v", err)
	}

	// Gather with explicit [minDesiredPodCount, maxDesiredPodCount] = [2, 3] should also succeed
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action":             "gather",
			"minDesiredPodCount": 2,
			"maxDesiredPodCount": 3,
			"timeout":            "1s",
			"refreshInterval":    "10ms",
		},
	})
	if err != nil {
		t.Fatalf("expected gather with [2, 3] range to succeed when 3 pods exist, got: %v", err)
	}

	// Gather with no toleration (requiring 4 pods) should time out when only 3 exist
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"action":          "gather",
			"timeout":         50 * time.Millisecond,
			"refreshInterval": 10 * time.Millisecond,
		},
	})
	if err == nil {
		t.Fatalf("expected timeout error when 4 pods expected and only 3 exist, got nil")
	}
}

func TestWaitForRunningPods_RangeAndToleration(t *testing.T) {
	objects := []runtime.Object{
		createTestRunningPod("pod-1", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-2", "test-ns", map[string]string{"app": "foo"}),
		createTestRunningPod("pod-3", "test-ns", map[string]string{"app": "foo"}),
	}

	fakeClient := newFakePodClient(objects...)
	multiClientSet := framework.NewMultiClientSetFromClients(fakeClient)
	clusterFramework := framework.NewFrameworkFromClients(multiClientSet, nil)

	m := createWaitForRunningPodsMeasurement()

	// 1. Explicit [minDesiredPodCount, maxDesiredPodCount] = [2, 4] without desiredPodCount
	_, err := m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"namespace":          "test-ns",
			"labelSelector":      "app=foo",
			"minDesiredPodCount": 2,
			"maxDesiredPodCount": 4,
			"timeout":            "1s",
			"refreshInterval":    "10ms",
		},
	})
	if err != nil {
		t.Fatalf("expected WaitForRunningPods with [2, 4] range to succeed for 3 running pods, got: %v", err)
	}

	// 2. desiredPodCount = 4 with toleration = 25% ([3, 5])
	_, err = m.Execute(&measurement.Config{
		ClusterFramework: clusterFramework,
		Params: map[string]interface{}{
			"namespace":       "test-ns",
			"labelSelector":   "app=foo",
			"desiredPodCount": 4,
			"toleration":      25.0,
			"timeout":         "1s",
			"refreshInterval": "10ms",
		},
	})
	if err != nil {
		t.Fatalf("expected WaitForRunningPods with desiredPodCount=4 and 25%% toleration to succeed for 3 running pods, got: %v", err)
	}
}


