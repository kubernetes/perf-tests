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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

const maxRetries = 5

// CreatePods creates pods in a user defined namspace with user configurable tuning sets
func CreatePods(f *framework.Framework, name, namespace string, labels labels.Set, spec v1.PodSpec, maxCount int, tuning *TuningSet) error {
	for i := 0; i < maxCount; i++ {
		framework.Logf("%v/%v : Creating pod", i+1, maxCount)
		podObj := newPod(name, namespace, i, labels, spec)
		if _, err := createNewPodWithRetries(f, namespace, podObj); err != nil {
			return err
		}
		if tuning == nil {
			continue
		}
		// If a rate limit has been defined we wait for N ms between creation
		if err := tuning.Pods.Delay(); err != nil {
			return err
		}
		// If a stepping tuningset has been defined in the config, we wait for the step of pods to be created, and pause
		if tuning.Pods.Stepping.StepSize != 0 && (i+1)%tuning.Pods.Stepping.StepSize == 0 {
			verifyRunning := f.NewClusterVerification(
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				},
				framework.PodStateVerification{
					Selectors:   labels,
					ValidPhases: []v1.PodPhase{v1.PodRunning},
				},
			)

			duration, err := time.ParseDuration(tuning.Pods.Stepping.Timeout)
			if err != nil {
				framework.Logf("Timeout not set: %v", err)
			}
			pods, err := verifyRunning.WaitFor(i+1, duration)
			if err != nil {
				return err
			} else if len(pods) < i+1 {
				return fmt.Errorf("Only got %v out of %v", len(pods), i+1)
			}

			framework.Logf("We have created %d pods; sleep %v", i+1, duration)
			if err := tuning.Pods.Pause(); err != nil {
				return err
			}
		}
	}
	return nil
}

// createNewPodWithRetries uses polling to retry pod creation
func createNewPodWithRetries(f *framework.Framework, namespace string, podObj *v1.Pod) (pod *v1.Pod, err error) {
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		pod, err = f.ClientSet.Core().Pods(namespace).Create(podObj)
		if err == nil {
			break
		}
	}
	return
}

// newPod creates a new Pod config object
func newPod(name, namespace string, num int, labels map[string]string, spec v1.PodSpec) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(name+"-pod-%v", num),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: spec,
	}
}
