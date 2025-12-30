/*
Copyright 2021 The Kubernetes Authors.

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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsControlPlaneNode(t *testing.T) {
	testcases := map[string]struct {
		Name   string
		Labels map[string]string
		Taints []corev1.Taint
		expect bool
	}{
		"node with controlplane node-role key": {
			Labels: map[string]string{keyControlPlaneNodeLabelTaint: ""},
			expect: true,
		},
		"node with controlplane node-role key and value as true": {
			Labels: map[string]string{keyControlPlaneNodeLabelTaint: "true"},
			expect: true,
		},
		"node with controlplane node-role key and value as false": {
			Labels: map[string]string{keyControlPlaneNodeLabelTaint: "false"},
			expect: true,
		},
		"node without controlplane node-role": {
			Labels: map[string]string{},
			expect: false,
		},
		"node without controlplane node-role taint": {
			Taints: []corev1.Taint{
				{
					Key:    "some-other-taint",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expect: false,
		},
		"node with controlplane node-role taint": {
			Taints: []corev1.Taint{
				{
					Key:    keyControlPlaneNodeLabelTaint,
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expect: true,
		},
	}
	for _, tc := range testcases {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Labels: tc.Labels},
			Spec:       corev1.NodeSpec{Taints: tc.Taints},
		}
		result := IsControlPlaneNode(node)
		assert.Equal(t, tc.expect, result)
	}
}
