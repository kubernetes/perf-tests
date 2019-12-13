/*
Copyright 2017 The Kubernetes Authors.

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

package framework

import (
	"time"

	"k8s.io/kubernetes/test/e2e/framework"
)

// Delay acts on a TuningSetObject and will sleep for RateLimit.Delay
func (tuning *TuningSetObject) Delay() error {
	return sleep(tuning.RateLimit.Delay)
}

// Pause acts on a TuningSetObject and will sleep for Stepping.Pause
func (tuning *TuningSetObject) Pause() error {
	return sleep(tuning.Stepping.Pause)
}

func sleep(d string) error {
	if d != "" {
		duration, err := time.ParseDuration(d)
		if err != nil {
			return err
		}
		framework.Logf("Waiting for %v", duration)
		time.Sleep(duration)
	}
	return nil
}

// Get matches the name of the tuning set defined in the project and returns a pointer to the set
func (ts TuningSets) Get(name string) *TuningSet {
	if name == "" {
		return nil
	}
	// Iterate through defined tuningSets
	for _, v := range ts {
		// If we have a matching tuningSet keep it
		if v.Name == name {
			return &v
		}
	}
	framework.Logf("No tuning found for %q", name)
	return nil
}
