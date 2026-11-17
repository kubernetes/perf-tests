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

package prometheus

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestOperatorWatchdog(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: prometheusStatefulSet}}

	tests := []struct {
		name string
		// createAfterRestarts is the number of operator restarts after which the StatefulSet appears; -1 means never.
		createAfterRestarts int
		checks              int
		wantRestarts        int
	}{
		{name: "created at once", createAfterRestarts: 0, checks: 10, wantRestarts: 0},
		{name: "not yet a minute", createAfterRestarts: -1, checks: 2, wantRestarts: 0},
		{name: "created after one restart", createAfterRestarts: 1, checks: 10, wantRestarts: 1},
		{name: "never created", createAfterRestarts: -1, checks: 30, wantRestarts: operatorRestartLimit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewSimpleClientset()
			restarts := 0
			if tc.createAfterRestarts == 0 {
				_ = c.Tracker().Add(statefulSet)
			}
			c.PrependReactor("delete-collection", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
				restarts++
				if restarts == tc.createAfterRestarts {
					_ = c.Tracker().Add(statefulSet)
				}
				return true, nil, nil
			})

			w := &operatorWatchdog{}
			for range tc.checks {
				if err := w.check(context.Background(), c); err != nil {
					t.Fatal(err)
				}
			}
			if restarts != tc.wantRestarts {
				t.Errorf("got %d restarts, want %d", restarts, tc.wantRestarts)
			}
		})
	}
}
