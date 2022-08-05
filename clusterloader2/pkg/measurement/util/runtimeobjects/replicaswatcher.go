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
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
)

const (
	informerSyncTimeout = time.Minute
)

// ReplicasWatcher is a struct that allows to check a number of replicas at a given time.
// Usage:
// var rw ReplicasWatcher = (...)
// if err := rw.Start(stopCh); err != nil {
//   panic(err);
// }
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
	nodeSelector labels.Selector
	affinity     *corev1.Affinity

	mu       sync.Mutex
	replicas int
}

var _ ReplicasWatcher = &NodeCounter{}

// NewNodeCounter returns a new nodeCounter that return a number of objects matching nodeSelector and affinity.
func NewNodeCounter(client clientset.Interface, nodeSelector labels.Selector, affinity *corev1.Affinity) *NodeCounter {
	return &NodeCounter{
		client:       client,
		nodeSelector: nodeSelector,
		affinity:     affinity,
	}
}

func (n *NodeCounter) Start(stopCh <-chan struct{}) error {
	i := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = n.nodeSelector.String()
				return n.client.CoreV1().Nodes().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = n.nodeSelector.String()
				return n.client.CoreV1().Nodes().Watch(context.TODO(), options)
			},
		},
		func(oldObj, newObj interface{}) {
			if err := n.handleObject(oldObj, newObj); err != nil {
				klog.Errorf("Error while processing node: %v", err)
			}
		},
	)
	// StartAndSync blocks until elements from initial list call are processed.
	return informer.StartAndSync(i, stopCh, informerSyncTimeout)
}

func (n *NodeCounter) handleObject(oldObj, newObj interface{}) error {
	old, err := n.shouldRun(oldObj)
	if err != nil {
		return err
	}
	new, err := n.shouldRun(newObj)
	if err != nil {
		return err
	}
	if new == old {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if new && !old {
		n.replicas++
	} else {
		n.replicas--
	}
	return nil
}

func (n *NodeCounter) Replicas() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.replicas
}

func (n *NodeCounter) shouldRun(obj interface{}) (bool, error) {
	if obj == nil {
		return false, nil
	}
	node, ok := obj.(*corev1.Node)
	if !ok {
		return false, fmt.Errorf("unexpected type of obj: %v. got %T, want *corev1.Node", obj, obj)
	}
	matched, err := podMatchesNodeAffinity(n.affinity, node)
	return !node.Spec.Unschedulable && matched, err
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
