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
	"math"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
)

type poissonLoad struct {
	params *api.PoissonLoad
}

func newPoissonLoad(params *api.PoissonLoad) TuningSet {
	return &poissonLoad{
		params: params,
	}
}

func (rl *poissonLoad) Execute(actions []func()) {
	var wg wait.Group
	for i := range actions {
		wg.Start(actions[i])
		time.Sleep(interArrivalTimes(rl.params.RatePS))
	}
	wg.Wait()
}

//Simulating inter-arrival times in a Poisson process
func interArrivalTimes(RatePS float64) time.Duration {
	var duration, max_duration time.Duration
	p := rand.Float64()

	max_duration = time.Duration(int(float64(time.Minute))) //setting a bound for the largest inter-arrival interval in the Poisson process
	duration = time.Duration(int(float64(time.Second) * (-math.Log(1 - p)/RatePS)))

	if duration > max_duration{
		duration = time.Duration(int(float64(time.Nanosecond)))
	}

	return duration
}
