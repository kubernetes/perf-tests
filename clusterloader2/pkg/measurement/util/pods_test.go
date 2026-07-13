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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodsStartupStatusStringWithSurgedRollingUpdate(t *testing.T) {
	pods := []*corev1.Pod{
		runningAndReadyPod("updated-1"),
		runningAndReadyPod("updated-2"),
		runningAndReadyPod("old"),
	}
	isPodUpdated := func(pod *corev1.Pod) error {
		if pod.Name == "old" {
			return errors.New("pod has old template")
		}
		return nil
	}

	status := ComputePodsStartupStatus(pods, 2, isPodUpdated)

	assert.Equal(t, 3, status.Created)
	assert.Equal(t, 2, status.Expected)
	assert.Equal(t, 3, status.Running)
	assert.Equal(t, 2, status.RunningUpdated)

	got := status.String()
	want := "Pods: 3 created, 2 desired, 3 running (2 updated), 0 pending scheduled, 0 not scheduled, 0 inactive, 0 terminating, 0 unknown, 0 runningButNotReady "

	assert.Equal(t, want, got)
	assert.NotContains(t, got, "out of", "should not describe desired pods as a creation limit")
}

func runningAndReadyPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
