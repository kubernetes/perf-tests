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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectSelector_NilReceiver(t *testing.T) {
	var selector *ObjectSelector

	if str := selector.String(); str != "everything" {
		t.Fatalf("expected 'everything', got: %s", str)
	}

	opts := &metav1.ListOptions{LabelSelector: "test"}
	selector.ApplySelectors(opts)
	if opts.LabelSelector != "test" {
		t.Fatalf("expected options to remain unchanged, got: %v", opts)
	}
}
