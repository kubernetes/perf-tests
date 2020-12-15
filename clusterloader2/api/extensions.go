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

package api

import (
	"encoding/json"
	"fmt"
	"time"
)

// MarshalJSON marshals Duration to string format.
func (d *Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON unmarshals Duration from string.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("unmarshaling error: %v", err)
	}
	duration, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("parsing duration error: %v", err)
	}
	*d = Duration(duration)
	return nil
}

// String converts Duration to string format.
func (d *Duration) String() string {
	return d.ToTimeDuration().String()
}

// ToTimeDuration converts Duration to time.Duration.
func (d *Duration) ToTimeDuration() time.Duration {
	return time.Duration(*d)
}

// IsMeasurement returns true whether a step is a measurement-step.
func (s *Step) IsMeasurement() bool {
	return len(s.Measurements) > 0
}

// IsPhase returns true whether a step is a phase-step.
func (s *Step) IsPhase() bool {
	return len(s.Phases) > 0
}

// IsModule returns true whether a step is a module.
func (s *Step) IsModule() bool {
	return s.Module.Path != ""
}
