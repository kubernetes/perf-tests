/*
Copyright 2017 The Kubernetes Authors.

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

package monitors

import (
	"fmt"
	"sync"

	"k8s.io/client-go/tools/cache"

	"github.com/golang/glog"
)

const (
	setOperationType    = "set"
	deleteOperationType = "delete"
)

type workItem struct {
	operationType string
	key           string
	obj           interface{}
}

// NoStoreQueue implements k8s.io/client-go/tools/cache Store and Queue interfaces
type NoStoreQueue struct {
	sync.Mutex

	cond sync.Cond
	// queue of items to process
	queue []workItem
	// map from keys to indices of items to process. Indices are not modified and are always growing,
	// (i.e. next item gets an index of the last item + 1). To map it to "real" indices inside the queue
	// we also store the offset (explained below).
	indices map[string]int64
	// index that will be assigned to next item that'll be added to the queue
	nextIndex int64
	// index of the (last process item + 1) which is equivalent to the index of the first item currently in the queue.
	// This means that indices in the queue are [offset, offset+1, offset+2, ...].
	offset int64
}

var keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

// NewNoStoreQueue returns newly created NoStoreQueue
func NewNoStoreQueue() *NoStoreQueue {
	q := &NoStoreQueue{
		queue:   []workItem{},
		indices: map[string]int64{},
	}

	q.cond.L = q
	return q
}

func (q *NoStoreQueue) enqueueOrModifyItemLocked(key string, obj interface{}, operationType string) {
	if index, ok := q.indices[key]; ok {
		q.queue[int(int64(index)-q.offset)].operationType = operationType
		q.queue[int(int64(index)-q.offset)].obj = obj
		return
	}
	q.indices[key] = q.nextIndex
	q.nextIndex++
	q.queue = append(q.queue, workItem{
		operationType: operationType,
		key:           key,
		obj:           obj,
	})
}

// Add implements Add function from Store interface
func (q *NoStoreQueue) Add(obj interface{}) error {
	key, err := keyFunc(obj)
	if err != nil {
		return cache.KeyError{obj, err}
	}

	defer q.cond.Broadcast()
	q.Lock()
	defer q.Unlock()
	q.enqueueOrModifyItemLocked(key, obj, setOperationType)
	return nil
}

// Update implements Update function from Store interface
func (q *NoStoreQueue) Update(obj interface{}) error {
	return q.Add(obj)
}

// Delete implements Delete function from Store interface
func (q *NoStoreQueue) Delete(obj interface{}) error {
	key, err := keyFunc(obj)
	if err != nil {
		return cache.KeyError{obj, err}
	}

	defer q.cond.Broadcast()
	q.Lock()
	defer q.Unlock()
	q.enqueueOrModifyItemLocked(key, obj, deleteOperationType)
	return nil
}

// List implements List function from Store interface
func (q *NoStoreQueue) List() []interface{} {
	return []interface{}{}
}

// ListKeys implements ListKeys function from Store interface
func (q *NoStoreQueue) ListKeys() []string {
	return []string{}
}

// Get implements Get function from Store interface
func (q *NoStoreQueue) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return "", false, fmt.Errorf("Unimplemented Get")
}

// GetByKey implements GetByKey function from Store interface
func (q *NoStoreQueue) GetByKey(key string) (item interface{}, exists bool, err error) {
	return "", false, fmt.Errorf("Unimplemented GetByKey")
}

// Replace implements Replace function from Store interface
func (q *NoStoreQueue) Replace(list []interface{}, rv string) error {
	// Replace is called during re-lists, so we can't return "unimplemented" here.
	// It's safe to ignore this call, as worst thing that can happen is loosing
	// few calls. As this is best effort component, we don't care too much about that.
	// It's possible that we'll miss a Delete, but taking into account that it shouldn't happen too
	// often, and that single datapoint has around 100 bytes, we're fine with leaking this amount of
	// memory. Another possiblity would be to use the list to filter out existing keys, but that may
	// lead to invalid removals if Pod watch is lagging behind Event watch.
	return nil
}

// Resync implements Resync function from Store interface
func (q *NoStoreQueue) Resync() error {
	return fmt.Errorf("Unimplemented Resync")
}

// Pop implements Pop function from Queue interface
func (q *NoStoreQueue) Pop(process cache.PopProcessFunc) (interface{}, error) {
	var item workItem
	func() {
		q.Lock()
		defer q.Unlock()
		for len(q.queue) == 0 {
			q.cond.Wait()
		}
		item = q.queue[0]
		q.queue = q.queue[1:]
		q.offset++
		key, err := keyFunc(item.obj)
		if err != nil {
			glog.Errorf("Error in getting key for obj: %v", item.obj)
		} else {
			delete(q.indices, key)
		}
	}()
	err := process(item)
	if e, ok := err.(cache.ErrRequeue); ok {
		q.AddIfNotPresent(item)
		err = e.Err
	}
	return item, err
}

// AddIfNotPresent implements AddIfNotPresent function from Queue interface
func (q *NoStoreQueue) AddIfNotPresent(obj interface{}) error {
	key, err := keyFunc(obj)
	if err != nil {
		return cache.KeyError{obj, err}
	}

	defer q.cond.Broadcast()
	q.Lock()
	defer q.Unlock()

	if _, ok := q.indices[key]; ok {
		return nil
	}
	q.indices[key] = q.nextIndex
	q.nextIndex++
	q.queue = append(q.queue, workItem{
		operationType: deleteOperationType,
		key:           key,
		obj:           obj,
	})
	return nil
}

// HasSynced implements HasSynced function from Queue interface
func (q *NoStoreQueue) HasSynced() bool {
	return true
}

// Close implements Close function from Queue interface
func (q *NoStoreQueue) Close() {}
