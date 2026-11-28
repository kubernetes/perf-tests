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

package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/checker"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func TestWaitForControlledPodsGather(t *testing.T) {
	tests := []struct {
		name          string
		statuses      []objectStatus
		wantErrPrefix string
		wantErrors    []string
		noPodStatus   bool
	}{
		{
			name:     "all controllers running",
			statuses: []objectStatus{running, running},
		},
		{
			name:          "failed controller preserves cause",
			statuses:      []objectStatus{failed, running},
			wantErrPrefix: "failed objects statuses:",
			wantErrors:    []string{"test/controller-0: failed to list pods: index unavailable"},
		},
		{
			name:          "failure without pod snapshot preserves cause",
			statuses:      []objectStatus{failed, running},
			wantErrPrefix: "failed objects statuses:",
			wantErrors:    []string{"test/controller-0: failed to list pods: index unavailable"},
			noPodStatus:   true,
		},
		{
			name:          "multiple failures preserve all causes",
			statuses:      []objectStatus{failed, failed},
			wantErrPrefix: "failed objects statuses:",
			wantErrors: []string{
				"test/controller-0: failed to list pods: index unavailable",
				"test/controller-1: failed to list pods: index unavailable",
			},
		},
		{
			name:          "missing checker reports count mismatch",
			statuses:      []objectStatus{running},
			wantErrPrefix: "incorrect objects number: 1/2 ReplicationControllers are running with all pods",
		},
		{
			name:          "timeout takes precedence over failure",
			statuses:      []objectStatus{failed, timeout},
			wantErrPrefix: "1 objects timed out: ReplicationControllers: test/controller-1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			gvr := corev1.SchemeGroupVersion.WithResource("replicationcontrollers")
			objects := make([]runtime.Object, 2)
			w := &waitForControlledPodsRunningMeasurement{
				isRunning:  true,
				kind:       "ReplicationController",
				gvr:        gvr,
				selector:   util.NewObjectSelector(),
				objectKeys: sets.NewString(),
				checkerMap: checker.NewMap(),
			}
			// Seed the synchronized cache and completed checkers to exercise gather
			// without starting informers or waiting for real pods.
			for i := range objects {
				obj := &corev1.ReplicationController{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "test",
						Name:            fmt.Sprintf("controller-%d", i),
						ResourceVersion: "1",
					},
				}
				objects[i] = obj
				if err := w.updateCacheLocked(nil, obj); err != nil {
					t.Fatal(err)
				}
			}
			client := fake.NewSimpleDynamicClient(scheme, objects...)
			w.clusterFramework = framework.NewFrameworkFromClients(nil, framework.NewMultiDynamicClientFromClients(client))
			var wantFailedPods []failedPod
			for i, status := range tc.statuses {
				key := fmt.Sprintf("test/controller-%d", i)
				o := newObjectChecker(key)
				o.status = status
				if status == failed || status == timeout {
					o.err = fmt.Errorf("%s: failed to list pods: index unavailable", key)
				}
				if (status == failed || status == timeout) && !tc.noPodStatus {
					o.failedPods = &measurementutil.PodsStatus{Info: []*measurementutil.PodInfo{{
						Namespace: "test",
						Name:      fmt.Sprintf("pod-%d", i),
						Hostname:  "node-1",
						Status:    measurementutil.RunningButNotReady,
					}}}
					wantFailedPods = append(wantFailedPods, failedPod{
						Namespace:    "test",
						ControlledBy: key,
						Name:         fmt.Sprintf("pod-%d", i),
						Host:         "node-1",
						Status:       "RunningButNotReady",
					})
				}
				w.checkerMap.Add(key, o)
			}
			failedPods, err := w.gather(2 * checkControlledPodsInterval)
			if tc.wantErrPrefix == "" {
				assert.NoError(t, err)
			} else if assert.Error(t, err) {
				assert.True(t, strings.HasPrefix(err.Error(), tc.wantErrPrefix), "unexpected error: %v", err)
				for _, cause := range tc.wantErrors {
					assert.Contains(t, err.Error(), cause)
				}
			}
			assert.ElementsMatch(t, wantFailedPods, failedPods)
		})
	}
}
