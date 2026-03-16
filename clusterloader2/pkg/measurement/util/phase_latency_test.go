/*
Copyright 2026 The Kubernetes Authors.

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

package util

import (
	"reflect"
	"testing"
	"time"
)

func TestGetOrderedKeys(t *testing.T) {
	ott := NewObjectTransitionTimes("test")
	ott.Set("pod3", "phase1", time.Unix(300, 0))
	ott.Set("pod1", "phase1", time.Unix(100, 0))
	ott.Set("pod2", "phase1", time.Unix(200, 0))
	ott.Set("pod4", "phase2", time.Unix(50, 0))

	got := ott.GetOrderedKeys("phase1")
	want := []KeyTimestamp{
		{Key: "pod1", Timestamp: time.Unix(100, 0)},
		{Key: "pod2", Timestamp: time.Unix(200, 0)},
		{Key: "pod3", Timestamp: time.Unix(300, 0)},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetOrderedKeys() = %v, want %v", got, want)
	}

	gotEmpty := ott.GetOrderedKeys("non-existent")
	if len(gotEmpty) != 0 {
		t.Errorf("GetOrderedKeys() for non-existent phase = %v, want empty", gotEmpty)
	}
}
