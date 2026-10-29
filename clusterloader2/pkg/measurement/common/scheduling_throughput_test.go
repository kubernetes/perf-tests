/*
Copyright 2026 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func TestSchedulingThroughput_InitialPodsSpike(t *testing.T) {
	client := fake.NewSimpleClientset()

	// Prepend reactor to set ResourceVersion on PodList so that PodStore sync can complete.
	client.Fake.PrependReactor("list", "pods", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
		obj, err := client.Tracker().List(gvr, gvk, ns)
		if err != nil {
			return true, nil, err
		}
		if list, ok := obj.(*corev1.PodList); ok {
			list.ListMeta.ResourceVersion = "12345"
			return true, list, nil
		}
		return true, obj, nil
	})

	// 1. Create 10 pods that are already scheduled/running.
	for i := 0; i < 10; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("initial-pod-%d", i),
				Namespace: "default",
				Labels:    map[string]string{"group": "load"},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1", // Mark scheduled
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		}
		_, err := client.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	s := createSchedulingThroughputMeasurement().(*schedulingThroughputMeasurement)
	selector := &util.ObjectSelector{
		Namespace:     "default",
		LabelSelector: "group=load",
	}

	// Start measurement with 1s interval.
	err := s.start(client, selector, 1*time.Second, "")
	assert.NoError(t, err)

	// Wait 1.5 seconds.
	time.Sleep(1500 * time.Millisecond)

	// Gather results.
	summaries, err := s.gather(0, "test")
	assert.NoError(t, err)
	assert.Len(t, summaries, 1)

	// Parse the summary content.
	var throughput schedulingThroughput
	err = json.Unmarshal([]byte(summaries[0].SummaryContent()), &throughput)
	assert.NoError(t, err)

	// With the bug, throughput max/percentiles will be 10 pods/sec because of the 10 initial pods,
	// even though NO new pods were scheduled during the measurement.
	// We want to assert that the throughput is 0.
	assert.Equal(t, float64(0), throughput.Max, "max throughput should be 0 since no new pods were scheduled during measurement")
	assert.Equal(t, float64(0), throughput.Perc99, "perc99 throughput should be 0")
}
