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
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	clutil "k8s.io/perf-tests/clusterloader2/pkg/util"
)

func newReadyNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Labels:          labels,
			ResourceVersion: "1",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func newFakeClient(objs ...runtime.Object) *fake.Clientset {
	client := fake.NewSimpleClientset(objs...)
	client.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		gvk := schema.GroupVersionKind{Version: "v1", Kind: "Node"}
		res, err := client.Tracker().List(action.GetResource(), gvk, action.(k8stesting.ListAction).GetNamespace())
		if err != nil {
			return true, nil, err
		}
		if list, ok := res.(*corev1.NodeList); ok {
			list.ListMeta.ResourceVersion = "1"
			return true, list, nil
		}
		return false, res, nil
	})
	return client
}

func TestWaitForNodes_WithinTolerationTimeout(t *testing.T) {
	client := newFakeClient(newReadyNode("node-1", map[string]string{"app": "test"}))

	selector := clutil.NewObjectSelector()
	_ = selector.Parse(map[string]interface{}{"labelSelector": "app=test"})

	options := &WaitForNodeOptions{
		Selector:             selector,
		MinDesiredNodeCount:  1,
		MaxDesiredNodeCount:  1,
		CallerName:           "Test",
		WaitForNodesInterval: 10 * time.Millisecond,
		TolerationTimeout:    200 * time.Millisecond,
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	err := WaitForNodes(client, stopCh, options)
	if err != nil {
		t.Fatalf("expected nil error when nodes are ready before tolerationTimeout, got: %v", err)
	}
}

func TestWaitForNodes_AfterTolerationTimeoutBeforeStopCh(t *testing.T) {
	client := newFakeClient()

	selector := clutil.NewObjectSelector()
	_ = selector.Parse(map[string]interface{}{"labelSelector": "app=test"})

	options := &WaitForNodeOptions{
		Selector:             selector,
		MinDesiredNodeCount:  1,
		MaxDesiredNodeCount:  1,
		CallerName:           "Test",
		WaitForNodesInterval: 20 * time.Millisecond,
		TolerationTimeout:    100 * time.Millisecond,
	}

	stopCh := make(chan struct{})
	time.AfterFunc(1*time.Second, func() {
		close(stopCh)
	})

	// Create node AFTER tolerationTimeout elapses (at ~200ms)
	time.AfterFunc(200*time.Millisecond, func() {
		_, _ = client.CoreV1().Nodes().Create(context.TODO(), newReadyNode("node-1", map[string]string{"app": "test"}), metav1.CreateOptions{})
	})

	err := WaitForNodes(client, stopCh, options)
	if err == nil {
		t.Fatalf("expected error when nodes become ready after tolerationTimeout, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "reached after tolerationTimeout") {
		t.Errorf("expected error message to contain 'reached after tolerationTimeout', got: %v", errMsg)
	}
	if !strings.Contains(errMsg, "delay after tolerationTimeout") {
		t.Errorf("expected error message to contain 'delay after tolerationTimeout', got: %v", errMsg)
	}
}

func TestWaitForNodes_TimeoutAfterStopCh(t *testing.T) {
	client := newFakeClient()

	selector := clutil.NewObjectSelector()
	_ = selector.Parse(map[string]interface{}{"labelSelector": "app=test"})

	options := &WaitForNodeOptions{
		Selector:             selector,
		MinDesiredNodeCount:  1,
		MaxDesiredNodeCount:  1,
		CallerName:           "Test",
		WaitForNodesInterval: 20 * time.Millisecond,
		TolerationTimeout:    100 * time.Millisecond,
	}

	stopCh := make(chan struct{})
	time.AfterFunc(200*time.Millisecond, func() {
		close(stopCh)
	})

	err := WaitForNodes(client, stopCh, options)
	if err == nil {
		t.Fatalf("expected timeout error when nodes never become ready, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "timeout while waiting for") {
		t.Errorf("expected standard timeout error message, got: %v", errMsg)
	}
}
