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
	"time"

	"k8s.io/perf-tests/clusterloader2/api"
)

func newTimer(ch chan<- struct{}, ts *api.TuningSet) *time.Timer {
	var t *time.Timer
	t = time.AfterFunc(ts.InitialDelay, func() {
		// TODO: Implement other tuning sets.
		d := 10 * time.Millisecond
		ch <- struct{}{}
		t.Reset(d)
	})
	return t
}
