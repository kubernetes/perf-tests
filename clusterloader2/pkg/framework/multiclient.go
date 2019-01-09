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

package framework

import (
	"fmt"
	"sync"

	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/config"
)

// MultiClientSet is a set of kubernetes clients.
type MultiClientSet struct {
	lock    sync.Mutex
	clients []clientset.Interface
	current int
}

// NewMultiClientSet creates new MultiClientSet for given kubeconfig and number.
func NewMultiClientSet(kubeconfigPath string, number int) (*MultiClientSet, error) {
	m := MultiClientSet{
		clients: make([]clientset.Interface, number),
	}
	for i := 0; i < number; i++ {
		conf, err := config.PrepareConfig(kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("config prepare failed: %v", err)
		}
		if number < 1 {
			return nil, fmt.Errorf("incorrect clients number")
		}
		m.clients[i], err = clientset.NewForConfig(conf)
		if err != nil {
			return nil, fmt.Errorf("creating clientset failed: %v", err)
		}
	}
	return &m, nil
}

// GetClient return one client instance from the set using round robin.
func (m *MultiClientSet) GetClient() clientset.Interface {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.current = (m.current + 1) % len(m.clients)
	return m.clients[m.current]
}

// MultiDynamicClient is a set of dynamic client.
type MultiDynamicClient struct {
	lock    sync.Mutex
	clients []dynamic.Interface
	current int
}

// NewMultiDynamicClient creates new MultiDynamicClient for given kubeconfig and number.
func NewMultiDynamicClient(kubeconfigPath string, number int) (*MultiDynamicClient, error) {
	m := MultiDynamicClient{
		clients: make([]dynamic.Interface, number),
	}
	for i := 0; i < number; i++ {
		conf, err := config.PrepareConfig(kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("config prepare failed: %v", err)
		}
		if number < 1 {
			return nil, fmt.Errorf("incorrect clients number")
		}
		m.clients[i], err = dynamic.NewForConfig(conf)
		if err != nil {
			return nil, fmt.Errorf("creating dynamic config failed: %v", err)
		}
	}
	return &m, nil
}

// GetClient return one client instance from the set using round robin.
func (m *MultiDynamicClient) GetClient() dynamic.Interface {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.current = (m.current + 1) % len(m.clients)
	return m.clients[m.current]
}
