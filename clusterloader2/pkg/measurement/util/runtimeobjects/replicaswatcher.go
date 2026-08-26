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

package runtimeobjects

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

// ReplicasWatcher is a struct that allows to check a number of replicas at a given time.
// Usage:
// var rw ReplicasWatcher = (...)
//
//	if err := rw.Start(stopCh); err != nil {
//	  panic(err);
//	}
//
// // Get number of replicas as needed.
// val = rw.Replicas()
// ...
// val = rw.Replicas()
type ReplicasWatcher interface {
	Replicas() int
	// Start must block until Replicas() returns a correct value.
	Start(stopCh <-chan struct{}) error
}

// ConstReplicas is a ReplicasWatcher implementation that returns a constant value.
type ConstReplicas struct {
	ReplicasCount int
}

func (c *ConstReplicas) Replicas() int {
	return c.ReplicasCount
}

func (c *ConstReplicas) Start(_ <-chan struct{}) error {
	return nil
}

var _ ReplicasWatcher = &ConstReplicas{}

// NodeCounter counts a number of node objects matching nodeSelector and affinity.
type NodeCounter struct {
	client       clientset.Interface
	nodeIndexer  cache.Indexer
	nodeSelector labels.Selector
	affinity     *corev1.Affinity
	mu           sync.Mutex
	tolerations  []corev1.Toleration
}

var _ ReplicasWatcher = &NodeCounter{}

// NewNodeCounter returns a new nodeCounter that return a number of objects matching nodeSelector and affinity.
func NewNodeCounter(client clientset.Interface, nodeSelector labels.Selector, affinity *corev1.Affinity, tolerations []corev1.Toleration) *NodeCounter {
	return &NodeCounter{
		client:       client,
		nodeSelector: nodeSelector,
		affinity:     affinity,
		tolerations:  tolerations,
	}
}

func (n *NodeCounter) Start(stopCh <-chan struct{}) error {
	indexer, err := util.NodeIndexerFactory.NodeIndexer(n.client)
	if err != nil {
		return fmt.Errorf("failed to get shared node indexer: %w", err)
	}

	n.mu.Lock()
	n.nodeIndexer = indexer
	n.mu.Unlock()

	return nil
}

func (n *NodeCounter) getIndexer() (cache.Indexer, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.nodeIndexer != nil {
		return n.nodeIndexer, nil
	}

	indexer, err := util.NodeIndexerFactory.NodeIndexer(n.client)
	if err != nil {
		return nil, fmt.Errorf("failed to get shared node indexer: %w", err)
	}

	n.nodeIndexer = indexer
	return n.nodeIndexer, nil
}

func (n *NodeCounter) Replicas() int {
	indexer, err := n.getIndexer()
	if err != nil {
		klog.Errorf("failed to get shared node indexer: %v", err)
		return 0
	}

	count := 0
	for _, obj := range indexer.List() {
		match, err := n.ShouldRun(obj)
		if err != nil {
			klog.Errorf("Error while processing node: %v", err)
			continue
		}

		if match {
			count++
		}
	}

	return count
}

func (n *NodeCounter) ShouldRun(obj interface{}) (bool, error) {
	if obj == nil {
		return false, nil
	}

	node, ok := obj.(*corev1.Node)
	if !ok {
		return false, fmt.Errorf("unexpected type of obj: %v. got %T, want *corev1.Node", obj, obj)
	}

	if n.nodeSelector != nil && !n.nodeSelector.Matches(labels.Set(node.Labels)) {
		return false, nil
	}

	matched, err := podMatchesNodeAffinity(n.affinity, node)
	if err != nil {
		return false, err
	}

	// refer to k8s.io/kubernetes@v1.22.15/pkg/controller/nodelifecycle/node_lifecycle_controller.go:633
	// refer to k8s.io/kubernetes@v1.22.15/pkg/controller/daemon/daemon_controller.go:1247
	_, hasUntoleratedTaint := corev1helpers.FindMatchingUntoleratedTaint(klog.Background(), node.Spec.Taints, n.tolerations, func(t *corev1.Taint) bool {
		return t.Effect == corev1.TaintEffectNoExecute || t.Effect == corev1.TaintEffectNoSchedule
	}, false)

	return !hasUntoleratedTaint && matched, nil
}

// GetReplicasOnce starts ReplicasWatcher and gets a number of replicas.
func GetReplicasOnce(rw ReplicasWatcher) (int, error) {
	stopCh := make(chan struct{})
	defer close(stopCh)
	if err := rw.Start(stopCh); err != nil {
		return 0, err
	}
	return rw.Replicas(), nil
}
