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

// PVCsStartupStatus represents phase of a pvc group.
type PVCsStartupStatus struct {
	Pending  int
	Bound    int
	Lost     int
	Expected int
	Created  int
}

// String returns string representation for PVCsStartupStatus.
func (s *PVCsStartupStatus) String() string {
	return fmt.Sprintf("PVCs: %d out of %d created, %d bound, %d pending, %d lost", s.Created, s.Expected, s.Bound, s.Pending, s.Lost)
}

// ComputePVCsStartupStatus computes PVCsStartupStatus for a group of PVCs.
func ComputePVCsStartupStatus(pvcs []*corev1.PersistentVolumeClaim, expected int) PVCsStartupStatus {
	startupStatus := PVCsStartupStatus{
		Expected: expected,
	}
	for _, p := range pvcs {
		startupStatus.Created++
		if p.Status.Phase == corev1.ClaimPending {
			startupStatus.Pending++
		} else if p.Status.Phase == corev1.ClaimBound {
			startupStatus.Bound++
		} else if p.Status.Phase == corev1.ClaimLost {
			startupStatus.Lost++
		}
	}
	return startupStatus
}

type pvcInfo struct {
	oldPhase string
	phase    string
}

// PVCDiff represets diff between old and new group of pvcs.
type PVCDiff map[string]*pvcInfo

// Print formats and prints the give PVCDiff.
func (p PVCDiff) String(ignorePhases sets.String) string {
	ret := ""
	for name, info := range p {
		if ignorePhases.Has(info.phase) {
			continue
		}
		if info.phase == nonExist {
			ret += fmt.Sprintf("PVC %v was deleted, had phase %v\n", name, info.oldPhase)
			continue
		}
		msg := fmt.Sprintf("PVC %v ", name)
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

// DeletedPVCs returns a slice of PVCs that were present at the beginning
// and then disappeared.
func (p PVCDiff) DeletedPVCs() []string {
	var deletedPVCs []string
	for pvcName, pvcInfo := range p {
		if pvcInfo.phase == nonExist {
			deletedPVCs = append(deletedPVCs, pvcName)
		}
	}
	return deletedPVCs
}

// AddedPVCs returns a slice of PVCs that were added.
func (p PVCDiff) AddedPVCs() []string {
	var addedPVCs []string
	for pvcName, pvcInfo := range p {
		if pvcInfo.oldPhase == nonExist {
			addedPVCs = append(addedPVCs, pvcName)
		}
	}
	return addedPVCs
}

// DiffPVCs computes a PVCDiff given 2 lists of PVCs.
func DiffPVCs(oldPVCs []*corev1.PersistentVolumeClaim, curPVCs []*corev1.PersistentVolumeClaim) PVCDiff {
	pvcInfoMap := PVCDiff{}

	// New PVCs will show up in the curPVCs list but not in oldPVCs. They have oldhostname/phase == nonexist.
	for _, pvc := range curPVCs {
		pvcInfoMap[pvc.Name] = &pvcInfo{phase: string(pvc.Status.Phase), oldPhase: nonExist}
	}

	// Deleted PVCs will show up in the oldPVCs list but not in curPVCs. They have a hostname/phase == nonexist.
	for _, pvc := range oldPVCs {
		if info, ok := pvcInfoMap[pvc.Name]; ok {
			info.oldPhase = string(pvc.Status.Phase)
		} else {
			pvcInfoMap[pvc.Name] = &pvcInfo{phase: nonExist, oldPhase: string(pvc.Status.Phase)}
		}
	}
	return pvcInfoMap
}
