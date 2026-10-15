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

package tuningset

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/perf-tests/clusterloader2/api"
)

func countingActions(n int, counter *int32) []func() {
	actions := make([]func(), n)
	for i := range actions {
		actions[i] = func() { atomic.AddInt32(counter, 1) }
	}
	return actions
}

func TestCreateTuningSet(t *testing.T) {
	testcases := map[string]struct {
		tuningSet *api.TuningSet
		expected  TuningSet
		wantErr   bool
	}{
		"qps load": {
			tuningSet: &api.TuningSet{QPSLoad: &api.QPSLoad{QPS: 10}},
			expected:  &qpsLoad{},
		},
		"randomized load": {
			tuningSet: &api.TuningSet{RandomizedLoad: &api.RandomizedLoad{AverageQPS: 10}},
			expected:  &randomizedLoad{},
		},
		"stepped load": {
			tuningSet: &api.TuningSet{SteppedLoad: &api.SteppedLoad{BurstSize: 1}},
			expected:  &steppedLoad{},
		},
		"time limited load": {
			tuningSet: &api.TuningSet{TimeLimitedLoad: &api.TimeLimitedLoad{}},
			expected:  &timeLimitedLoad{},
		},
		"randomized time limited load": {
			tuningSet: &api.TuningSet{RandomizedTimeLimitedLoad: &api.RandomizedTimeLimitedLoad{}},
			expected:  &randomizedTimeLimitedLoad{},
		},
		"parallelism limited load": {
			tuningSet: &api.TuningSet{ParallelismLimitedLoad: &api.ParallelismLimitedLoad{ParallelismLimit: 1}},
			expected:  &parallelismLimitedLoad{},
		},
		"global qps load": {
			tuningSet: &api.TuningSet{GlobalQPSLoad: &api.GlobalQPSLoad{QPS: 10, Burst: 1}},
			expected:  &globalQPSLoad{},
		},
		"poisson load": {
			tuningSet: &api.TuningSet{PoissonLoad: &api.PoissonLoad{ExpectedActionsPerSecond: 10}},
			expected:  &poissonLoad{},
		},
		"no load defined": {
			tuningSet: &api.TuningSet{},
			wantErr:   true,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			tc.tuningSet.Name = name
			factory := NewFactory()
			factory.Init([]*api.TuningSet{tc.tuningSet})
			ts, err := factory.CreateTuningSet(name)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.IsType(t, tc.expected, ts)
		})
	}
}

func TestCreateTuningSetNotFound(t *testing.T) {
	factory := NewFactory()
	factory.Init([]*api.TuningSet{{Name: "first", QPSLoad: &api.QPSLoad{QPS: 10}}})
	// Init replaces the previously registered tuning sets.
	factory.Init([]*api.TuningSet{{Name: "second", QPSLoad: &api.QPSLoad{QPS: 10}}})

	_, err := factory.CreateTuningSet("first")
	assert.Error(t, err)
	_, err = factory.CreateTuningSet("unknown")
	assert.Error(t, err)
	_, err = factory.CreateTuningSet("second")
	assert.NoError(t, err)
}

func TestGlobalQPSLoadIsSharedByName(t *testing.T) {
	factory := NewFactory()
	factory.Init([]*api.TuningSet{
		{Name: "a", GlobalQPSLoad: &api.GlobalQPSLoad{QPS: 10, Burst: 1}},
		{Name: "b", GlobalQPSLoad: &api.GlobalQPSLoad{QPS: 10, Burst: 1}},
	})
	a1, err := factory.CreateTuningSet("a")
	assert.NoError(t, err)
	a2, err := factory.CreateTuningSet("a")
	assert.NoError(t, err)
	b, err := factory.CreateTuningSet("b")
	assert.NoError(t, err)

	assert.Same(t, a1, a2)
	assert.NotSame(t, a1, b)
}

func TestExecuteRunsEveryActionOnce(t *testing.T) {
	testcases := map[string]TuningSet{
		"qps load":                     newQPSLoad(&api.QPSLoad{QPS: 1000}),
		"randomized load":              newRandomizedLoad(&api.RandomizedLoad{AverageQPS: 1000}),
		"stepped load":                 newSteppedLoad(&api.SteppedLoad{BurstSize: 5, StepDelay: api.Duration(time.Millisecond)}),
		"time limited load":            newTimeLimitedLoad(&api.TimeLimitedLoad{TimeLimit: api.Duration(10 * time.Millisecond)}),
		"randomized time limited load": newRandomizedTimeLimitedLoad(&api.RandomizedTimeLimitedLoad{TimeLimit: api.Duration(10 * time.Millisecond)}),
		"parallelism limited load":     newParallelismLimitedLoad(&api.ParallelismLimitedLoad{ParallelismLimit: 3}),
		"global qps load":              newGlobalQPSLoad(&api.GlobalQPSLoad{QPS: 1000, Burst: 1}),
		"poisson load":                 newPoissonLoad(&api.PoissonLoad{ExpectedActionsPerSecond: 1000}),
	}
	for name, ts := range testcases {
		for _, n := range []int{0, 1, 25} {
			t.Run(fmt.Sprintf("%s/%d actions", name, n), func(t *testing.T) {
				var counter int32
				ts.Execute(countingActions(n, &counter))
				assert.Equal(t, int32(n), atomic.LoadInt32(&counter))
			})
		}
	}
}

func TestParallelismLimitedLoadRespectsLimit(t *testing.T) {
	const limit = 3
	var running, maxRunning int32
	actions := make([]func(), 20)
	for i := range actions {
		actions[i] = func() {
			cur := atomic.AddInt32(&running, 1)
			for {
				prev := atomic.LoadInt32(&maxRunning)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxRunning, prev, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&running, -1)
		}
	}

	newParallelismLimitedLoad(&api.ParallelismLimitedLoad{ParallelismLimit: limit}).Execute(actions)

	assert.LessOrEqual(t, atomic.LoadInt32(&maxRunning), int32(limit))
	assert.Greater(t, atomic.LoadInt32(&maxRunning), int32(1))
}

func TestSteppedLoadPausesAfterEachBurst(t *testing.T) {
	const stepDelay = 20 * time.Millisecond
	var counter int32
	start := time.Now()
	// 7 actions in bursts of 3 pause after the 3rd and the 6th action.
	newSteppedLoad(&api.SteppedLoad{BurstSize: 3, StepDelay: api.Duration(stepDelay)}).Execute(countingActions(7, &counter))

	assert.GreaterOrEqual(t, time.Since(start), 2*stepDelay)
	assert.Equal(t, int32(7), atomic.LoadInt32(&counter))
}

func TestTimeLimitedLoadSpreadsActionsOverTimeLimit(t *testing.T) {
	const timeLimit = 50 * time.Millisecond
	var counter int32
	start := time.Now()
	newTimeLimitedLoad(&api.TimeLimitedLoad{TimeLimit: api.Duration(timeLimit)}).Execute(countingActions(5, &counter))

	assert.GreaterOrEqual(t, time.Since(start), timeLimit)
}

func TestRandomizedTimeLimitedLoadZeroTimeLimit(t *testing.T) {
	var counter int32
	// rand.Int63n panics for non-positive arguments, so zero time limit must be handled.
	assert.NotPanics(t, func() {
		newRandomizedTimeLimitedLoad(&api.RandomizedTimeLimitedLoad{}).Execute(countingActions(5, &counter))
	})
	assert.Equal(t, int32(5), atomic.LoadInt32(&counter))
}

func TestSleepDurationRange(t *testing.T) {
	const avgQPS = 10.0
	upperBound := time.Duration(2 * float64(time.Second) / avgQPS)
	for i := 0; i < 1000; i++ {
		d := sleepDuration(avgQPS)
		assert.GreaterOrEqual(t, d, time.Duration(0))
		assert.Less(t, d, upperBound)
	}
}

func TestInterArrivalTimeNonNegative(t *testing.T) {
	for i := 0; i < 1000; i++ {
		assert.GreaterOrEqual(t, interArrivalTime(10), time.Duration(0))
	}
}
