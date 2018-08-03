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

package common

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func init() {
	measurement.Register("Timer", createTimerMeasurment)
}

func createTimerMeasurment() measurement.Measurement {
	return &timer{}
}

type timer struct {
	startTime time.Time
}

// Execute supports two actions. start - which start timer. stop - which stops timer
// and prints to log time duration between start and stop.
// Stop action requires label parameter to be provided.
func (t *timer) Execute(config *measurement.MeasurementConfig) error {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return err
	}

	switch action {
	case "start":
		t.startTime = time.Now()
	case "stop":
		label, err := util.GetString(config.Params, "label")
		if err != nil {
			return err
		}
		if t.startTime.IsZero() {
			return fmt.Errorf("uninitialized timer")
		}
		glog.Infof("%s: %v", label, time.Since(t.startTime))
		t.startTime = time.Time{}
	default:
		return fmt.Errorf("unknown action %s", action)
	}
	return nil
}
