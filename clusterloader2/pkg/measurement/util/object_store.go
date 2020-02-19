/*
Copyright 2018 The Kubernetes Authors.

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

/*
This file is copy of https://github.com/kubernetes/kubernetes/blob/master/test/utils/pod_store.go
with slight changes regarding labelSelector and flagSelector applied.
*/

package util

import (
	"fmt"
	"reflect"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// ObjectStore is a convenient wrapper around cache.Store.
type ObjectStore struct {
	cache.Store
	stopCh    chan struct{}
	Reflector *cache.Reflector
}

// newObjectStore creates ObjectStore based on given object selector.
func newObjectStore(obj runtime.Object, lw *cache.ListWatch, selector *ObjectSelector) (*ObjectStore, error) {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	stopCh := make(chan struct{})
	name := fmt.Sprintf("%sStore: %s", reflect.TypeOf(obj).String(), selector.String())
	reflector := cache.NewNamedReflector(name, lw, obj, store, 0)
	go reflector.Run(stopCh)
	if err := wait.PollImmediate(50*time.Millisecond, 2*time.Minute, func() (bool, error) {
		if len(reflector.LastSyncResourceVersion()) != 0 {
			return true, nil
		}
		return false, nil
	}); err != nil {
		close(stopCh)
		return nil, fmt.Errorf("couldn't initialize %s: %v", name, err)
	}
	return &ObjectStore{
		Store:     store,
		stopCh:    stopCh,
		Reflector: reflector,
	}, nil
}

// Stop stops ObjectStore watch.
func (s *ObjectStore) Stop() {
	close(s.stopCh)
}

// PodStore is a convenient wrapper around cache.Store.
type PodStore struct {
	*ObjectStore
}

// NewPodStore creates PodStore based on given object selector.
func NewPodStore(c clientset.Interface, selector *ObjectSelector) (*PodStore, error) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().Pods(selector.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().Pods(selector.Namespace).Watch(options)
		},
	}
	objectStore, err := newObjectStore(&v1.Pod{}, lw, selector)
	if err != nil {
		return nil, err
	}
	return &PodStore{ObjectStore: objectStore}, nil
}

// List returns list of pods (that satisfy conditions provided to NewPodStore).
func (s *PodStore) List() []*v1.Pod {
	objects := s.Store.List()
	pods := make([]*v1.Pod, 0, len(objects))
	for _, o := range objects {
		pods = append(pods, o.(*v1.Pod))
	}
	return pods
}

// PVCStore is a convenient wrapper around cache.Store.
type PVCStore struct {
	*ObjectStore
}

// NewPVCStore creates PVCStore based on a given object selector.
func NewPVCStore(c clientset.Interface, selector *ObjectSelector) (*PVCStore, error) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().PersistentVolumeClaims(selector.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().PersistentVolumeClaims(selector.Namespace).Watch(options)
		},
	}
	objectStore, err := newObjectStore(&v1.PersistentVolumeClaim{}, lw, selector)
	if err != nil {
		return nil, err
	}
	return &PVCStore{ObjectStore: objectStore}, nil
}

// List returns list of pvcs (that satisfy conditions provided to NewPVCStore).
func (s *PVCStore) List() []*v1.PersistentVolumeClaim {
	objects := s.Store.List()
	pvcs := make([]*v1.PersistentVolumeClaim, 0, len(objects))
	for _, o := range objects {
		pvcs = append(pvcs, o.(*v1.PersistentVolumeClaim))
	}
	return pvcs
}

// PVStore is a convenient wrapper around cache.Store.
type PVStore struct {
	*ObjectStore
}

// NewPVStore creates PVStore based on a given object selector.
func NewPVStore(c clientset.Interface, selector *ObjectSelector) (*PVStore, error) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().PersistentVolumes().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().PersistentVolumes().Watch(options)
		},
	}
	objectStore, err := newObjectStore(&v1.PersistentVolume{}, lw, selector)
	if err != nil {
		return nil, err
	}
	return &PVStore{ObjectStore: objectStore}, nil
}

// List returns list of pvs (that satisfy conditions provided to NewPVStore).
func (s *PVStore) List() []*v1.PersistentVolume {
	objects := s.Store.List()
	pvs := make([]*v1.PersistentVolume, 0, len(objects))
	for _, o := range objects {
		pvs = append(pvs, o.(*v1.PersistentVolume))
	}
	return pvs
}

// NodeStore is a convenient wrapper around cache.Store.
type NodeStore struct {
	*ObjectStore
}

// NewNodeStore creates NodeStore based on a given object selector.
func NewNodeStore(c clientset.Interface, selector *ObjectSelector) (*NodeStore, error) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().Nodes().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return c.CoreV1().Nodes().Watch(options)
		},
	}
	objectStore, err := newObjectStore(&v1.Node{}, lw, selector)
	if err != nil {
		return nil, err
	}
	return &NodeStore{ObjectStore: objectStore}, nil
}

// List returns list of nodes that satisfy conditions provided to NewNodeStore.
func (s *NodeStore) List() []*v1.Node {
	objects := s.Store.List()
	nodes := make([]*v1.Node, 0, len(objects))
	for _, o := range objects {
		nodes = append(nodes, o.(*v1.Node))
	}
	return nodes
}
