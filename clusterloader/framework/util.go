/*
Copyright 2016 The Kubernetes Authors.

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
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

const maxRetries = 5

// CreatePods creates pods in user defined namspaces with user configurable tuning sets
func CreatePods(f *framework.Framework, appName string, ns string, labels map[string]string, spec v1.PodSpec, maxCount int, tuning *TuningSetType) {
	for i := 0; i < maxCount; i++ {
		framework.Logf("%v/%v : Creating pod", i+1, maxCount)
		// Retry on pod creation failure
		for retryCount := 0; retryCount < maxRetries; retryCount++ {
			_, err := f.ClientSet.Core().Pods(ns).Create(&v1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name:      fmt.Sprintf(appName+"-pod-%v", i),
					Namespace: ns,
					Labels:    labels,
				},
				Spec: spec,
			})
			if err == nil {
				break
			}
			framework.ExpectNoError(err)
		}
		if tuning != nil {
			// If a rate limit has been defined we wait for N ms between creation
			if tuning.Pods.RateLimit.Delay != 0 {
				framework.Logf("Sleeping %d ms between podcreation.", tuning.Pods.RateLimit.Delay)
				time.Sleep(tuning.Pods.RateLimit.Delay * time.Millisecond)
			}
			// If a stepping tuningset has been defined in the config, we wait for the step of pods to be created, and pause
			if tuning.Pods.Stepping.StepSize != 0 && (i+1)%tuning.Pods.Stepping.StepSize == 0 {
				verifyRunning := f.NewClusterVerification(
					&v1.Namespace{
						ObjectMeta: v1.ObjectMeta{
							Name: ns,
						},
						Status: v1.NamespaceStatus{},
					},
					framework.PodStateVerification{
						Selectors:   labels,
						ValidPhases: []v1.PodPhase{v1.PodRunning},
					},
				)

				pods, err := verifyRunning.WaitFor(i+1, tuning.Pods.Stepping.Timeout*time.Second)
				if err != nil {
					framework.Failf("Error in wait... %v", err)
				} else if len(pods) < i+1 {
					framework.Failf("Only got %v out of %v", len(pods), i+1)
				}

				framework.Logf("We have created %d pods and are now sleeping for %d seconds", i+1, tuning.Pods.Stepping.Pause)
				time.Sleep(tuning.Pods.Stepping.Pause * time.Second)
			}
		}
	}
}
