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
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// PodStore is a convenient wrapper around cache.Store that returns list of v1.Pod instead of interface{}.
type PodStore struct {
	cache.Store
	stopCh    chan struct{}
	Reflector *cache.Reflector
}

// NewPodStore creates PodStore based on given namespace, label selector and field selector.
func NewPodStore(c clientset.Interface, namespace string, labelSelector string, fieldSelector string) (*PodStore, error) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector
			options.FieldSelector = fieldSelector
			obj, err := c.CoreV1().Pods(namespace).List(options)
			return runtime.Object(obj), err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector
			options.FieldSelector = fieldSelector
			return c.CoreV1().Pods(namespace).Watch(options)
		},
	}
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	stopCh := make(chan struct{})
	name := fmt.Sprintf("PodStore: %s", CreateSelectorsString(namespace, labelSelector, fieldSelector))
	reflector := cache.NewNamedReflector(name, lw, &v1.Pod{}, store, 0)
	go reflector.Run(stopCh)
	if err := wait.PollImmediate(50*time.Millisecond, 2*time.Minute, func() (bool, error) {
		if len(reflector.LastSyncResourceVersion()) != 0 {
			return true, nil
		}
		return false, nil
	}); err != nil {
		close(stopCh)
		return nil, err
	}
	return &PodStore{Store: store, stopCh: stopCh, Reflector: reflector}, nil
}

// List returns to list of pods (that satisfy conditions provided to NewPodStore).
func (s *PodStore) List() []*v1.Pod {
	objects := s.Store.List()
	pods := make([]*v1.Pod, 0)
	for _, o := range objects {
		pods = append(pods, o.(*v1.Pod))
	}
	return pods
}

// Stop stops podstore watch.
func (s *PodStore) Stop() {
	close(s.stopCh)
}
