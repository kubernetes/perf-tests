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

package ticker

import (
	"fmt"
	"math/rand"
	"time"

	"k8s.io/perf-tests/clusterloader2/api"
)

func newTimer(ch chan<- struct{}, ts *api.TuningSet) (*time.Timer, error) {
	var t *time.Timer
	getTicks, err := getTicksFunc(ts)
	if err != nil {
		return nil, err
	}
	getDelay, err := getDelayFunc(ts)
	if err != nil {
		return nil, err
	}
	t = time.AfterFunc(ts.InitialDelay, func() {
		for i := int32(0); i < getTicks(); i++ {
			ch <- struct{}{}
		}
		t.Reset(getDelay())
	})
	return t, nil
}

// getTicksFunc returns function that specifies number of ticks that should be provided after delay.
func getTicksFunc(ts *api.TuningSet) (func() int32, error) {
	// For QpsLoad and RandomizedLoad, one tick is created at a time.
	if ts.QpsLoad != nil || ts.RandomizedLoad != nil {
		return func() int32 { return int32(1) }, nil
	}
	// For SteppedLoad, 'BurstSize' many ticks are created at a time.
	if ts.SteppedLoad != nil {
		return func() int32 { return ts.SteppedLoad.BurstSize }, nil
	}
	return nil, fmt.Errorf("unknown tuningset kind: %v", ts)
}

// getDelayFunc returns function that specifies delay between ticks based on given tuning set.
func getDelayFunc(ts *api.TuningSet) (func() time.Duration, error) {
	if ts.QpsLoad != nil {
		// For QpsLoad function will always return constant delay between ticks,
		// that satisfy required Qps.
		return func() time.Duration {
			return time.Duration(int(float64(time.Second) / ts.QpsLoad.Qps))
		}, nil
	}
	if ts.RandomizedLoad != nil {
		// For RandomizedLoad function will return random delay from range [0, 2q],
		// where q = 1 / AverageQps is average delay between ticks.
		return func() time.Duration {
			randomFactor := 2 * rand.Float64()
			return time.Duration(int(randomFactor * float64(time.Second) / ts.RandomizedLoad.AverageQps))
		}, nil
	}
	if ts.SteppedLoad != nil {
		// For SteppedLoad function will always return constant delay between bursts.
		return func() time.Duration {
			return ts.SteppedLoad.StepDelay
		}, nil
	}
	return nil, fmt.Errorf("unknown tuningset kind: %v", ts)
}
