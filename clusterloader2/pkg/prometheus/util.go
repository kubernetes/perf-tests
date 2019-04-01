/*
Copyright 2019 The Kubernetes Authors.

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
	"encoding/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

type targetsResponse struct {
	Data targetsData `json:"data""`
}

type targetsData struct {
	ActiveTargets []Target `json:"activeTargets"`
}

// Target represents a Prometheus target object.
type Target struct {
	Labels map[string]string `json:"labels"`
	Health string            `json:"health"`
}

// CheckTargetsReady returns true iff there is at least minExpectedTargets matching the selector and
// all of them are ready.
func CheckTargetsReady(k8sClient kubernetes.Interface, selector func(Target) bool, minExpectedTargets int) (bool, error) {
	raw, err := k8sClient.CoreV1().
		Services(namespace).
		ProxyGet("http", "prometheus-k8s", "9090", "api/v1/targets", nil /*params*/).
		DoRaw()
	if err != nil {
		// This might happen if prometheus server is temporary down, log error but don't return it.
		klog.Warningf("error while calling prometheus api: %v", err)
		return false, nil
	}
	var response targetsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, err // This shouldn't happen, return error.
	}
	if len(response.Data.ActiveTargets) < minExpectedTargets {
		klog.Infof("Not enough active targets (%d), expected at least (%d), waiting for more to become active...",
			len(response.Data.ActiveTargets), minExpectedTargets)
		return false, nil
	}

	nReady := 0
	for _, t := range response.Data.ActiveTargets {
		if !selector(t) {
			continue
		}
		if t.Health == "up" {
			nReady++
		}
	}
	if nReady < len(response.Data.ActiveTargets) {
		klog.Infof("%d/%d targets are ready", nReady, len(response.Data.ActiveTargets))
		return false, nil
	}
	klog.Infof("All %d targets are ready", len(response.Data.ActiveTargets))
	return true, nil
}
