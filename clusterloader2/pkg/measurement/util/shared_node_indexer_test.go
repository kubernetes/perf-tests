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
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSharedNodeIndexerFactory_Lifecycle(t *testing.T) {
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: map[string]string{"env": "prod"},
		},
		Spec: corev1.NodeSpec{
			Unschedulable: false,
		},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-2",
			Labels: map[string]string{"env": "test"},
		},
		Spec: corev1.NodeSpec{
			Unschedulable: true,
		},
	}

	fakeClient := fake.NewSimpleClientset(node1, node2)
	factory := NewSharedNodeIndexerFactory()
	defer factory.Stop()

	// Initial fetch
	inf1, err := factory.NodeInformer(fakeClient)
	if err != nil {
		t.Fatalf("expected no error from NodeInformer, got: %v", err)
	}

	// Singleton verification
	inf2, err := factory.NodeInformer(fakeClient)
	if err != nil {
		t.Fatalf("expected no error from second NodeInformer call, got: %v", err)
	}
	if inf1 != inf2 {
		t.Fatalf("expected identical informer instances from singleton factory")
	}

	indexer, err := factory.NodeIndexer(fakeClient)
	if err != nil {
		t.Fatalf("expected no error from NodeIndexer, got: %v", err)
	}
	if len(indexer.List()) != 2 {
		t.Fatalf("expected 2 indexed nodes, got: %d", len(indexer.List()))
	}

	// Reset / Stop
	factory.Reset()
	if factory.synced || factory.nodeInformer != nil {
		t.Fatalf("expected factory to be fully cleared after Reset")
	}
}

func TestSharedNodeIndexerFactory_ConcurrentAccess(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-concurrent"},
	}
	fakeClient := fake.NewSimpleClientset(node)
	factory := NewSharedNodeIndexerFactory()
	defer factory.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx, err := factory.NodeIndexer(fakeClient)
			if err != nil {
				t.Errorf("concurrent NodeIndexer error: %v", err)
				return
			}
			if len(idx.List()) == 0 {
				t.Errorf("expected at least 1 node listed")
			}
		}()
	}
	wg.Wait()
}

func TestSharedNodeIndexerFactory_StructLiteral_LazyInit(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-literal"},
	}
	fakeClient := fake.NewSimpleClientset(node)

	// Direct struct literal without calling NewSharedNodeIndexerFactory
	factory := &SharedNodeIndexerFactory{}
	defer factory.Stop()

	indexer, err := factory.NodeIndexer(fakeClient)
	if err != nil {
		t.Fatalf("expected no error from struct literal factory, got: %v", err)
	}
	if len(indexer.List()) != 1 {
		t.Fatalf("expected 1 node, got: %d", len(indexer.List()))
	}
}
