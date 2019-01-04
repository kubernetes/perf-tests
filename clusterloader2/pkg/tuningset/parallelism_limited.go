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

	"k8s.io/client-go/util/workqueue"
	"k8s.io/perf-tests/clusterloader2/api"
)

type parallelismLimitedLoad struct {
	params *api.ParallelismLimitedLoad
}

func newParallelismLimitedLoad(params *api.ParallelismLimitedLoad) TuningSet {
	return &parallelismLimitedLoad{
		params: params,
	}
}

func (p *parallelismLimitedLoad) Execute(actions []func()) {
	executeAction := func(i int) {
		actions[i]()
	}
	workqueue.ParallelizeUntil(context.TODO(), int(p.params.ParallelismLimit), len(actions), executeAction)
}
