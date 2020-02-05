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
	"fmt"
	"regexp"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const allTargets = -1

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

// CheckAllTargetsReady returns true iff there is at least minActiveTargets matching the selector and
// all of them are ready.
func CheckAllTargetsReady(k8sClient kubernetes.Interface, selector func(Target) bool, minActiveTargets int) (bool, error) {
	return CheckTargetsReady(k8sClient, selector, minActiveTargets, allTargets)
}

// CheckTargetsReady returns true iff there is at least minActiveTargets matching the selector and
// at least minReadyTargets of them are ready.
func CheckTargetsReady(k8sClient kubernetes.Interface, selector func(Target) bool, minActiveTargets, minReadyTargets int) (bool, error) {
	raw, err := k8sClient.CoreV1().
		Services(namespace).
		ProxyGet("http", "prometheus-k8s", "9090", "api/v1/targets", nil /*params*/).
		DoRaw()
	if err != nil {
		response := "(empty)"
		if raw != nil {
			response = string(raw)
		}
		// This might happen if prometheus server is temporary down, log error but don't return it.
		klog.Warningf("error while calling prometheus api: %v, response: %q", err, response)
		return false, nil
	}
	var response targetsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, err // This shouldn't happen, return error.
	}
	nReady, nTotal := 0, 0
	var exampleNotReadyTarget Target
	for _, t := range response.Data.ActiveTargets {
		if !selector(t) {
			continue
		}
		nTotal++
		if t.Health == "up" {
			nReady++
			continue
		}
		exampleNotReadyTarget = t
	}
	if nTotal < minActiveTargets {
		klog.Infof("Not enough active targets (%d), expected at least (%d), waiting for more to become active...",
			nTotal, minActiveTargets)
		return false, nil
	}
	if minReadyTargets == allTargets {
		minReadyTargets = nTotal
	}
	if nReady < minReadyTargets {
		klog.Infof("%d/%d targets are ready, example not ready target: %v", nReady, minReadyTargets, exampleNotReadyTarget)
		return false, nil
	}
	klog.Infof("All %d expected targets are ready", minReadyTargets)
	return true, nil
}

const snapshotNamePattern = `^(?:[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?)$`

var re = regexp.MustCompile(snapshotNamePattern)

// VerifySnapshotName verifies if snapshot name statisfies snapshot name regex.
func VerifySnapshotName(name string) error {
	if re.MatchString(name) {
		return nil
	}
	return fmt.Errorf("disk name doesn't match %v", snapshotNamePattern)
}
