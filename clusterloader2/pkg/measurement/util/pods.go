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

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	nonExist = "NonExist"
)

type status int

const (
	Unknown status = iota
	Terminating
	RunningAndReady
	RunningButNotReady
	PendingScheduled
	PendingNotScheduled
	Inactive
)

// PodsStartupStatus represents status of a pods group.
type PodsStartupStatus struct {
	Expected              int
	Terminating           int
	Running               int
	Scheduled             int
	RunningButNotReady    int
	Waiting               int
	Pending               int
	Unknown               int
	Inactive              int
	Created               int
	RunningUpdated        int
	LastIsPodUpdatedError error
}

// String returns string representation for podsStartupStatus.
func (s *PodsStartupStatus) String() string {
	return fmt.Sprintf("Pods: %d out of %d created, %d running (%d updated), %d pending scheduled, %d not scheduled, %d inactive, %d terminating, %d unknown, %d runningButNotReady ",
		s.Created, s.Expected, s.Running, s.RunningUpdated, s.Pending, s.Waiting, s.Inactive, s.Terminating, s.Unknown, s.RunningButNotReady)
}

func podStatus(p *corev1.Pod) status {
	if p.DeletionTimestamp != nil {
		return Terminating
	}
	if p.Status.Phase == corev1.PodRunning {
		ready := false
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if ready {
			// Only count a pod is running when it is also ready.
			return RunningAndReady
		}
		return RunningButNotReady
	}
	if p.Status.Phase == corev1.PodPending {
		if p.Spec.NodeName == "" {
			return PendingNotScheduled
		}
		return PendingScheduled
	}
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return Inactive
	}
	return Unknown
}

// ComputePodsStartupStatus computes PodsStartupStatus for a group of pods.
// TODO(mborsz): Migrate to podStatus instead of recalculating per pod status here.
func ComputePodsStartupStatus(pods []*corev1.Pod, expected int, isPodUpdated func(*corev1.Pod) error) PodsStartupStatus {
	startupStatus := PodsStartupStatus{
		Expected: expected,
	}
	for _, p := range pods {
		if p.DeletionTimestamp != nil {
			startupStatus.Terminating++
			continue
		}
		startupStatus.Created++
		if p.Status.Phase == corev1.PodRunning {
			ready := false
			for _, c := range p.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if ready {
				// Only count a pod is running when it is also ready.
				startupStatus.Running++
				if isPodUpdated == nil {
					startupStatus.RunningUpdated++
				} else {
					if err := isPodUpdated(p); err != nil {
						startupStatus.LastIsPodUpdatedError = err
					} else {
						startupStatus.RunningUpdated++
					}
				}
			} else {
				startupStatus.RunningButNotReady++
			}
		} else if p.Status.Phase == corev1.PodPending {
			if p.Spec.NodeName == "" {
				startupStatus.Waiting++
			} else {
				startupStatus.Pending++
			}
		} else if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			startupStatus.Inactive++
		} else if p.Status.Phase == corev1.PodUnknown {
			startupStatus.Unknown++
		}
		if p.Spec.NodeName != "" {
			startupStatus.Scheduled++
		}
	}
	return startupStatus
}

type podDiffInfo struct {
	oldHostname string
	oldPhase    string
	hostname    string
	phase       string
}

// PodDiff represets diff between old and new group of pods.
type PodDiff map[string]*podDiffInfo

// Print formats and prints the give PodDiff.
func (p PodDiff) String(ignorePhases sets.String) string {
	ret := ""
	for name, info := range p {
		if ignorePhases.Has(info.phase) {
			continue
		}
		if info.phase == nonExist {
			ret += fmt.Sprintf("Pod %v was deleted, had phase %v and host %v\n", name, info.oldPhase, info.oldHostname)
			continue
		}
		phaseChange, hostChange := false, false
		msg := fmt.Sprintf("Pod %v ", name)
		if info.oldPhase != info.phase {
			phaseChange = true
			if info.oldPhase == nonExist {
				msg += fmt.Sprintf("in phase %v ", info.phase)
			} else {
				msg += fmt.Sprintf("went from phase: %v -> %v ", info.oldPhase, info.phase)
			}
		}
		if info.oldHostname != info.hostname {
			hostChange = true
			if info.oldHostname == nonExist || info.oldHostname == "" {
				msg += fmt.Sprintf("assigned host %v ", info.hostname)
			} else {
				msg += fmt.Sprintf("went from host: %v -> %v ", info.oldHostname, info.hostname)
			}
		}
		if phaseChange || hostChange {
			ret += msg + "\n"
		}
	}
	return ret
}

// DeletedPods returns a slice of pods that were present at the beginning
// and then disappeared.
func (p PodDiff) DeletedPods() []string {
	var deletedPods []string
	for podName, podDiffInfo := range p {
		if podDiffInfo.hostname == nonExist {
			deletedPods = append(deletedPods, podName)
		}
	}
	return deletedPods
}

// AddedPods returns a slice of pods that were added.
func (p PodDiff) AddedPods() []string {
	var addedPods []string
	for podName, podDiffInfo := range p {
		if podDiffInfo.oldHostname == nonExist {
			addedPods = append(addedPods, podName)
		}
	}
	return addedPods
}

// DiffPods computes a PodDiff given 2 lists of pods.
func DiffPods(oldPods []*corev1.Pod, curPods []*corev1.Pod) PodDiff {
	podDiffInfoMap := PodDiff{}

	// New pods will show up in the curPods list but not in oldPods. They have oldhostname/phase == nonexist.
	for _, pod := range curPods {
		podDiffInfoMap[pod.Name] = &podDiffInfo{hostname: pod.Spec.NodeName, phase: string(pod.Status.Phase), oldHostname: nonExist, oldPhase: nonExist}
	}

	// Deleted pods will show up in the oldPods list but not in curPods. They have a hostname/phase == nonexist.
	for _, pod := range oldPods {
		if info, ok := podDiffInfoMap[pod.Name]; ok {
			info.oldHostname, info.oldPhase = pod.Spec.NodeName, string(pod.Status.Phase)
		} else {
			podDiffInfoMap[pod.Name] = &podDiffInfo{hostname: nonExist, phase: nonExist, oldHostname: pod.Spec.NodeName, oldPhase: string(pod.Status.Phase)}
		}
	}
	return podDiffInfoMap
}

type podInfo struct {
	name     string
	hostname string
	phase    string
	status   status
}

func (p *podInfo) String() string {
	return fmt.Sprintf("{%v %v %v %v}", p.name, p.phase, p.status, p.hostname)
}

// PodsStatus is a collection of current pod phases and node assignments data.
type PodsStatus struct {
	info []*podInfo
}

// ComputePodsStatus computes PodsStatus for a group of pods.
func ComputePodsStatus(pods []*corev1.Pod) *PodsStatus {
	ps := &PodsStatus{
		info: make([]*podInfo, len(pods)),
	}
	for i := range pods {
		ps.info[i] = &podInfo{
			name:     pods[i].Name,
			hostname: pods[i].Spec.NodeName,
			phase:    string(pods[i].Status.Phase),
			status:   podStatus(pods[i]),
		}
	}
	return ps
}

// String returns string representation of a PodsStatus.
func (ps *PodsStatus) String() string {
	return fmt.Sprintf("%v", ps.info)
}

func (ps *PodsStatus) NotRunningAndReady() *PodsStatus {
	res := &PodsStatus{
		info: make([]*podInfo, 0),
	}

	for _, info := range ps.info {
		if info.status != RunningAndReady {
			res.info = append(res.info, info)
		}
	}

	return res
}
