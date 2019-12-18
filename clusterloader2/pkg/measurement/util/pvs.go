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

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// PVsStartupStatus represents phase of a PV group.
type PVsStartupStatus struct {
	Pending   int
	Available int
	Bound     int
	Released  int
	Failed    int
	Expected  int
	Created   int
}

// String returns string representation for PVsStartupStatus.
func (s *PVsStartupStatus) String() string {
	return fmt.Sprintf("PVs: %d out of %d created, %d bound, %d pending, %d available, %d failed",
		s.Created, s.Expected, s.Bound, s.Pending, s.Available, s.Failed)
}

// ComputePVsStartupStatus computes PVsStartupStatus for a group of PVs.
func ComputePVsStartupStatus(PVs []*corev1.PersistentVolume, expected int) PVsStartupStatus {
	startupStatus := PVsStartupStatus{
		Expected: expected,
	}
	for _, p := range PVs {
		startupStatus.Created++
		if p.Status.Phase == corev1.VolumePending {
			startupStatus.Pending++
		} else if p.Status.Phase == corev1.VolumeAvailable {
			startupStatus.Available++
		} else if p.Status.Phase == corev1.VolumeBound {
			startupStatus.Bound++
		} else if p.Status.Phase == corev1.VolumeReleased {
			startupStatus.Released++
		} else if p.Status.Phase == corev1.VolumeFailed {
			startupStatus.Failed++
		}
	}
	return startupStatus
}

type pvInfo struct {
	oldPhase string
	phase    string
}

// PVDiff represents diff between old and new group of PVs.
type PVDiff map[string]*pvInfo

// Print formats and prints the given PVDiff.
func (p PVDiff) String(ignorePhases sets.String) string {
	ret := ""
	for name, info := range p {
		if ignorePhases.Has(info.phase) {
			continue
		}
		if info.phase == nonExist {
			ret += fmt.Sprintf("PV %v was deleted, had phase %v\n", name, info.oldPhase)
			continue
		}
		msg := fmt.Sprintf("PV %v ", name)
		if info.oldPhase != info.phase {
			if info.oldPhase == nonExist {
				msg += fmt.Sprintf("in phase %v ", info.phase)
			} else {
				msg += fmt.Sprintf("went from phase: %v -> %v ", info.oldPhase, info.phase)
			}
			ret += msg + "\n"
		}
	}
	return ret
}

// DeletedPVs returns a slice of PVs that were present at the beginning
// and then disappeared.
func (p PVDiff) DeletedPVs() []string {
	var deletedPVs []string
	for PVName, pvInfo := range p {
		if pvInfo.phase == nonExist {
			deletedPVs = append(deletedPVs, PVName)
		}
	}
	return deletedPVs
}

// AddedPVs returns a slice of PVs that were added.
func (p PVDiff) AddedPVs() []string {
	var addedPVs []string
	for PVName, pvInfo := range p {
		if pvInfo.oldPhase == nonExist {
			addedPVs = append(addedPVs, PVName)
		}
	}
	return addedPVs
}

// DiffPVs computes a PVDiff given 2 lists of PVs.
func DiffPVs(oldPVs []*corev1.PersistentVolume, curPVs []*corev1.PersistentVolume) PVDiff {
	pvInfoMap := PVDiff{}

	// New PVs will show up in the curPVs list but not in oldPVs. They have oldhostname/phase == nonexist.
	for _, PV := range curPVs {
		pvInfoMap[PV.Name] = &pvInfo{phase: string(PV.Status.Phase), oldPhase: nonExist}
	}

	// Deleted PVs will show up in the oldPVs list but not in curPVs. They have a hostname/phase == nonexist.
	for _, PV := range oldPVs {
		if info, ok := pvInfoMap[PV.Name]; ok {
			info.oldPhase = string(PV.Status.Phase)
		} else {
			pvInfoMap[PV.Name] = &pvInfo{phase: nonExist, oldPhase: string(PV.Status.Phase)}
		}
	}
	return pvInfoMap
}
