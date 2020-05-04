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
	"math/rand"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
)

type randomizedLoad struct {
	params *api.RandomizedLoad
}

func newRandomizedLoad(params *api.RandomizedLoad) TuningSet {
	return &randomizedLoad{
		params: params,
	}
}

func (rl *randomizedLoad) Execute(actions []func()) {
	var wg wait.Group
	for i := range actions {
		wg.Start(actions[i])
		time.Sleep(sleepDuration(rl.params.AverageQPS))
	}
	wg.Wait()
}

func sleepDuration(avgQPS float64) time.Duration {
	randomFactor := 2 * rand.Float64()
	return time.Duration(int(randomFactor * float64(time.Second) / avgQPS))
}
