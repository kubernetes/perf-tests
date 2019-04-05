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

package monitors

import (
	"fmt"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Fork from kubernetes/test/utils - necessary to make the types match...

func podRunningReady(p *v1.Pod) (bool, error) {
	// Check the phase is running.
	if p.Status.Phase != v1.PodRunning {
		return false, fmt.Errorf("want pod '%s' on '%s' to be '%v' but was '%v'",
			p.ObjectMeta.Name, p.Spec.NodeName, v1.PodRunning, p.Status.Phase)
	}
	// Check the ready condition is true.
	if !podReady(p) {
		return false, fmt.Errorf("pod '%s' on '%s' didn't have condition {%v %v}; conditions: %v",
			p.ObjectMeta.Name, p.Spec.NodeName, v1.PodReady, v1.ConditionTrue, p.Status.Conditions)
	}
	return true, nil
}

// PodRunningReadyOrSucceeded checks whether pod p's phase is running and it has a ready
// condition of status true or wheather the Pod already succeeded.
func PodRunningReadyOrSucceeded(p *v1.Pod) (bool, error) {
	// Check if the phase is succeeded.
	if p.Status.Phase == v1.PodSucceeded {
		return true, nil
	}
	return podRunningReady(p)
}

func podReady(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func getPodKey(pod *v1.Pod) string {
	return types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}.String()
}

func getPodKeyFromReference(objRef *v1.ObjectReference) string {
	return types.NamespacedName{Namespace: objRef.Namespace, Name: objRef.Name}.String()
}

func checkPodAndGetStartupLatency(p *v1.Pod) (bool, time.Time, time.Time) {
	if ok, _ := PodRunningReadyOrSucceeded(p); !ok {
		return false, time.Unix(0, 0), time.Unix(0, 0)
	}
	return true, p.CreationTimestamp.Time, time.Now()

}
