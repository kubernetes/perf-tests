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
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/client-go/tools/cache"
)

const (
	resyncPeriod = time.Duration(0)
)

// NewWatcherWithHandler creates a simple watcher that will call `h` for all coming objects
func NewWatcherWithHandler(lw cache.ListerWatcher, objType runtime.Object, setHandler, deleteHandler func(obj interface{}) error) cache.Controller {
	fifo := NewNoStoreQueue()

	cfg := &cache.Config{
		Queue:            fifo,
		ListerWatcher:    lw,
		ObjectType:       objType,
		FullResyncPeriod: resyncPeriod,
		RetryOnError:     false,

		Process: func(obj interface{}) error {
			workItem := obj.(workItem)
			switch workItem.operationType {
			case setOperationType:
				return setHandler(workItem.obj)
			case deleteOperationType:
				return deleteHandler(workItem.obj)
			default:
				return fmt.Errorf("Encountered unknown operation type: %v", workItem.operationType)
			}
		},
	}
	return cache.New(cfg)
}
