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

package workerqueue

import (
	"sync/atomic"
	"testing"
)

func TestWorkerQueue(t *testing.T) {
	// Create a queue with 10 background workers
	var q Interface = NewWorkerQueue(10)

	// Add 100 tasks to the queue
	var num int32
	for i := 0; i < 100; i++ {
		// 100 instances of a simple task, add 1 to num
		task := func() {
			// Workers all touch num at the same time, so need atomicity
			atomic.AddInt32(&num, 1)
		}
		q.Add(&task)
	}

	// Stop the queue
	q.Stop()

	// Verify that the tasks actually executed
	if got := atomic.LoadInt32(&num); got != 100 {
		t.Errorf("expected 100 tasks to be executed, got %d", got)
	}
}
