/*
Copyright 2019 The Kubernetes Authors.

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

package workerqueue

import (
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

// Interface of a workerqueue.
type Interface interface {
	Add(*func())
	Stop()
}

// WorkerQueue is worker group with a task queue.
type WorkerQueue struct {
	queue       workqueue.Interface
	workerGroup wait.Group
}

// NewWorkerQueue creates new WorkerQueue
// with worker group of given size.
func NewWorkerQueue(size int) Interface {
	wq := &WorkerQueue{
		queue: workqueue.New(),
	}
	for i := 0; i < size; i++ {
		wq.workerGroup.Start(wq.worker)
	}
	return wq
}

// Add adds a new task to the queue.
func (wq *WorkerQueue) Add(f *func()) {
	wq.queue.Add(f)
}

// Stop stops the working group.
func (wq *WorkerQueue) Stop() {
	wq.queue.ShutDown()
	wq.workerGroup.Wait()
}

func (wq *WorkerQueue) worker() {
	for {
		f, stop := wq.queue.Get()
		if stop {
			return
		}
		(*f.(*func()))()
		wq.queue.Done(f)
	}
}
