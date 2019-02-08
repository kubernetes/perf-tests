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
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
)

type randomizedTimeLimitedLoad struct {
	params *api.RandomizedTimeLimitedLoad
}

func newRandomizedTimeLimitedLoad(params *api.RandomizedTimeLimitedLoad) TuningSet {
	return &randomizedTimeLimitedLoad{
		params: params,
	}
}

type scheduleEntry struct {
	action    func()
	startTime time.Time
}

func (r *randomizedTimeLimitedLoad) Execute(actions []func()) {
	var schedule []scheduleEntry
	for i := range actions {
		schedule = append(schedule, scheduleEntry{
			action: actions[i],
			// assign random time in [now, now+TimeLimit]
			startTime: time.Now().Add(time.Duration(rand.Int63n(r.params.TimeLimit.ToTimeDuration().Nanoseconds()))),
		})
	}

	sort.Slice(schedule, func(i, j int) bool {
		return schedule[i].startTime.Before(schedule[j].startTime)
	})

	var wg wait.Group
	for i := 0; i < len(schedule); {
		now := time.Now()
		if schedule[i].startTime.Before(now) {
			wg.Start(schedule[i].action)
			i++
			continue
		}
		time.Sleep(schedule[i].startTime.Sub(now))
	}
	wg.Wait()
}
