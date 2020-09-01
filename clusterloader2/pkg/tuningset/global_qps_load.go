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

package tuningset

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/api"

	"golang.org/x/time/rate"
)

type globalQPSLoadFactory struct {
	existing map[string]*globalQPSLoad
	lock     sync.Mutex
}

func newGlobalQPSLoadFactory() *globalQPSLoadFactory {
	return &globalQPSLoadFactory{existing: make(map[string]*globalQPSLoad), lock: sync.Mutex{}}
}

func (f *globalQPSLoadFactory) GetOrCreate(name string, params *api.GlobalQPSLoad) *globalQPSLoad {
	f.lock.Lock()
	defer f.lock.Unlock()
	qps, ok := f.existing[name]
	if !ok {
		qps = newGlobalQPSLoad(params)
		f.existing[name] = qps
	}
	return qps
}

type globalQPSLoad struct {
	limiter *rate.Limiter
}

func newGlobalQPSLoad(params *api.GlobalQPSLoad) *globalQPSLoad {
	return &globalQPSLoad{
		limiter: rate.NewLimiter(rate.Limit(params.QPS), params.Burst),
	}
}

func (ql *globalQPSLoad) Execute(actions []func()) {
	var wg wait.Group
	for i := range actions {
		err := ql.limiter.Wait(context.TODO())
		if err != nil {
			klog.Errorf("Error while waiting for rate limitter: %v - skipping the action", err)
			continue
		}
		wg.Start(actions[i])
	}
	wg.Wait()
}
