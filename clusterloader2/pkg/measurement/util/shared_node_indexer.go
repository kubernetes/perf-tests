/*
Copyright 2026 The Kubernetes Authors.

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
	"fmt"
	"sync"
	"time"

	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
)

const defaultNodeInformerSyncTimeout = time.Minute

// NewSharedNodeIndexerFactory creates a new SharedNodeIndexerFactory instance.
func NewSharedNodeIndexerFactory() *SharedNodeIndexerFactory {
	f := &SharedNodeIndexerFactory{}
	f.initCond = sync.NewCond(&f.lock)
	return f
}

// NodeIndexerFactory is the process-wide shared node indexer factory.
var NodeIndexerFactory = NewSharedNodeIndexerFactory()

// SharedNodeIndexerFactory manages a shared Node informer and indexer instance across ClusterLoader2.
type SharedNodeIndexerFactory struct {
	lock         sync.Mutex
	initCond     *sync.Cond
	initializing bool
	client       clientset.Interface
	nodeInformer coreinformers.NodeInformer
	stopCh       chan struct{}
	synced       bool
}

func (s *SharedNodeIndexerFactory) condLocked() *sync.Cond {
	if s.initCond == nil {
		s.initCond = sync.NewCond(&s.lock)
	}

	return s.initCond
}

func (s *SharedNodeIndexerFactory) stopLocked() {
	for s.initializing {
		s.condLocked().Wait()
	}

	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil
	}

	s.nodeInformer = nil
	s.client = nil
	s.synced = false
}

// NodeInformer returns the shared corev1 NodeInformer, starting and syncing it if needed.
func (s *SharedNodeIndexerFactory) NodeInformer(c clientset.Interface) (coreinformers.NodeInformer, error) {
	s.lock.Lock()
	for s.initializing {
		s.condLocked().Wait()
	}

	if s.client != nil && s.client != c {
		s.stopLocked()
	}

	if s.synced && s.nodeInformer != nil {
		inf := s.nodeInformer
		s.lock.Unlock()
		return inf, nil
	}

	s.initializing = true
	s.lock.Unlock()

	stopCh := make(chan struct{})
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		c,
		0,
		informers.WithTransform(informer.TrimManagedFields),
	)
	nodeInformer := informerFactory.Core().V1().Nodes()
	nodeSynced := nodeInformer.Informer().HasSynced

	informerFactory.Start(stopCh)

	ctx, cancel := context.WithTimeout(context.Background(), defaultNodeInformerSyncTimeout)
	defer cancel()

	var syncErr error
	if !cache.WaitForNamedCacheSync("NodeIndexer", ctx.Done(), nodeSynced) {
		close(stopCh)
		syncErr = fmt.Errorf("failed to sync shared node informer within %v", defaultNodeInformerSyncTimeout)
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.initializing = false
	s.condLocked().Broadcast()

	if syncErr != nil {
		return nil, syncErr
	}

	s.client = c
	s.nodeInformer = nodeInformer
	s.stopCh = stopCh
	s.synced = true

	return s.nodeInformer, nil
}

// NodeIndexer returns the shared Node cache.Indexer.
func (s *SharedNodeIndexerFactory) NodeIndexer(c clientset.Interface) (cache.Indexer, error) {
	inf, err := s.NodeInformer(c)
	if err != nil {
		return nil, err
	}

	return inf.Informer().GetIndexer(), nil
}

// Stop stops the shared node informer and terminates background reflector routines.
func (s *SharedNodeIndexerFactory) Stop() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.stopLocked()
}

// Reset stops the existing informer and clears the factory state.
func (s *SharedNodeIndexerFactory) Reset() {
	s.Stop()
}
