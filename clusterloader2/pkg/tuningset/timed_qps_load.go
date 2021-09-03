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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
)

type timedQPSLoad struct {
	params *api.TimedQPSLoad
}

func newTimedQPSLoad(params *api.TimedQPSLoad) TuningSet {
	return &timedQPSLoad{
		params: params,
	}
}

func (ql *timedQPSLoad) Execute(actions []func()) {
	stopTime := time.Now().Add(time.Duration(ql.params.Duration))

	var wg wait.Group
	for time.Now().Before(stopTime) {
		// TODO:
		// * log time left in tuningset
		sleepDuration := time.Duration(int(float64(time.Second) / ql.params.QPS))
		for i := range actions {
			wg.Start(actions[i])
			time.Sleep(sleepDuration)
		}
	}
	wg.Wait()
}
