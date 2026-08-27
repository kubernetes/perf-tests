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
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
)

const defaultNodeInformerSyncTimeout = time.Minute

// NodeIndexerFactory is the process-wide shared node indexer factory.
var NodeIndexerFactory = &SharedNodeIndexerFactory{}

// SharedNodeIndexerFactory manages a shared Node informer and indexer instance across ClusterLoader2.
type SharedNodeIndexerFactory struct {
	nodeIndexer cache.Indexer
	err         error
	once        sync.Once
}

// NodeIndexer returns the shared Node cache.Indexer.
func (s *SharedNodeIndexerFactory) NodeIndexer(c clientset.Interface) (cache.Indexer, error) {
	s.once.Do(func() {
		s.nodeIndexer, s.err = s.start(c)
	})
	return s.nodeIndexer, s.err
}

func (s *SharedNodeIndexerFactory) start(c clientset.Interface) (cache.Indexer, error) {
	ctx := context.Background()
	informerFactory := informers.NewSharedInformerFactoryWithOptions(c, 0, informers.WithTransform(informer.TrimManagedFields))
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	informerFactory.Start(ctx.Done())

	ctxSync, cancel := context.WithTimeout(ctx, defaultNodeInformerSyncTimeout)
	defer cancel()

	if !cache.WaitForNamedCacheSync("NodeIndexer", ctxSync.Done(), nodeInformer.HasSynced) {
		return nil, fmt.Errorf("failed to sync shared node informer within %v", defaultNodeInformerSyncTimeout)
	}
	return nodeInformer.GetIndexer(), nil
}
