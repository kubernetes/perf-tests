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

type ticker struct {
	C     <-chan struct{}
	timer *time.Timer
}

func newTicker(ts *api.TuningSet) (*ticker, error) {
	ch := make(chan struct{})
	timer, err := newTimer(ch, ts)
	if err != nil {
		return nil, err
	}
	return &ticker{
		C:     ch,
		timer: timer,
	}, nil
}

// Stop stops the ticker.
func (t *ticker) Stop() {
	t.timer.Stop()
}
