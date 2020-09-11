/*
Copyright 2019 The Kubernetes Authors.

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

package gatherers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_collectMachines(t *testing.T) {
	nodeList := &corev1.NodeList{Items: []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Address: "10.0.0.1"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Address: "10.0.0.2"},
					{Address: "10.0.0.3"},
				},
			},
		},
	}}
	testcases := []struct {
		name      string
		nodeList  *corev1.NodeList
		masterIPs []string
		expResult []machine
	}{
		{
			name:      "empty lists",
			nodeList:  &corev1.NodeList{Items: nil},
			expResult: nil,
		},
		{
			name:     "some nodes",
			nodeList: nodeList,
			expResult: []machine{
				{nodeName: "node1", addresses: []string{"10.0.0.1"}},
				{nodeName: "node2", addresses: []string{"10.0.0.2", "10.0.0.3"}},
			},
		},
		{
			name:      "nodes mentioned in masters",
			nodeList:  nodeList,
			masterIPs: []string{"10.0.0.1"},
			expResult: []machine{
				{nodeName: "node1", addresses: []string{"10.0.0.1"}},
				{nodeName: "node2", addresses: []string{"10.0.0.2", "10.0.0.3"}},
			},
		},
		{
			name:      "unjoined masters",
			nodeList:  nodeList,
			masterIPs: []string{"10.0.1.1"},
			expResult: []machine{
				{nodeName: "node1", addresses: []string{"10.0.0.1"}},
				{nodeName: "node2", addresses: []string{"10.0.0.2", "10.0.0.3"}},
				{nodeName: "", sshIP: "10.0.1.1", addresses: []string{"10.0.1.1"}},
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			res := collectMachines(tc.nodeList, tc.masterIPs)
			if !cmp.Equal(res, tc.expResult, cmp.AllowUnexported(machine{})) {
				t.Errorf("machines did not match expected, diff (-want, +got): %v", cmp.Diff(res, tc.expResult, cmp.AllowUnexported(machine{})))
			}
		})
	}
}
