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
	"strings"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// WaitForPVCOptions is an options used by WaitForPVCs methods.
type WaitForPVCOptions struct {
	Selector            *util.ObjectSelector
	DesiredPVCCount     int
	CallerName          string
	WaitForPVCsInterval time.Duration
}

// WaitForPVCs waits till desired number of PVCs is running.
// PVCs are be specified by namespace, field and/or label selectors.
// If stopCh is closed before all PVCs are running, the error will be returned.
func WaitForPVCs(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForPVCOptions) error {
	ps, err := NewPVCStore(clientSet, options.Selector)
	if err != nil {
		return fmt.Errorf("PVC store creation error: %v", err)
	}
	defer ps.Stop()

	oldPVCs := ps.List()
	scaling := uninitialized
	var pvcsStatus PVCsStartupStatus

	switch {
	case len(oldPVCs) == options.DesiredPVCCount:
		scaling = none
	case len(oldPVCs) < options.DesiredPVCCount:
		scaling = up
	case len(oldPVCs) > options.DesiredPVCCount:
		scaling = down
	}

	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for %d PVCs to be running in namespace '%v' with labels '%v' and fields '%v' - only %d found bound",
				options.DesiredPVCCount, options.Selector.Namespace, options.Selector.LabelSelector, options.Selector.FieldSelector, pvcsStatus.Bound)
		case <-time.After(options.WaitForPVCsInterval):
			pvcs := ps.List()
			pvcsStatus = ComputePVCsStartupStatus(pvcs, options.DesiredPVCCount)

			diff := DiffPVCs(oldPVCs, pvcs)
			deletedPVCs := diff.DeletedPVCs()
			if scaling != down && len(deletedPVCs) > 0 {
				klog.Errorf("%s: %s: %d PVCs disappeared: %v", options.CallerName, options.Selector.String(), len(deletedPVCs), strings.Join(deletedPVCs, ", "))
			}
			addedPVCs := diff.AddedPVCs()
			if scaling != up && len(addedPVCs) > 0 {
				klog.Errorf("%s: %s: %d PVCs appeared: %v", options.CallerName, options.Selector.String(), len(deletedPVCs), strings.Join(deletedPVCs, ", "))
			}
			klog.V(2).Infof("%s: %s: %s", options.CallerName, options.Selector.String(), pvcsStatus.String())
			// We wait until there is a desired number of PVCs bound and all other PVCs are pending.
			if len(pvcs) == (pvcsStatus.Bound+pvcsStatus.Pending) && pvcsStatus.Bound == options.DesiredPVCCount {
				return nil
			}
			oldPVCs = pvcs
		}
	}
}
