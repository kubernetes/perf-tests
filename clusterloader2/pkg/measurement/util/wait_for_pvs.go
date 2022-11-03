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
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// WaitForPVOptions is an options used by WaitForPVs methods.
type WaitForPVOptions struct {
	Selector           *util.ObjectSelector
	DesiredPVCount     int
	Provisioner        string
	CallerName         string
	WaitForPVsInterval time.Duration
}

func (options WaitForPVOptions) filter() string {
	filter := options.Selector.String()
	if options.Provisioner != "" {
		filter += " && provisioner=" + options.Provisioner
	}
	return filter
}

// WaitForPVs waits till desired number of PVs is running.
// PVs are be specified by field and/or label selectors.
// If stopCh is closed before all PVs are running, the error will be returned.
func WaitForPVs(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForPVOptions) error {
	ps, err := NewPVStore(clientSet, options.Selector, options.Provisioner)
	if err != nil {
		return fmt.Errorf("PV store creation error: %v", err)
	}
	defer ps.Stop()

	oldPVs := ps.List()
	scaling := uninitialized
	var pvStatus PVsStartupStatus

	switch {
	case len(oldPVs) == options.DesiredPVCount:
		scaling = none
	case len(oldPVs) < options.DesiredPVCount:
		scaling = up
	case len(oldPVs) > options.DesiredPVCount:
		scaling = down
	}

	for {
		select {
		case <-stopCh:
			example := ""
			if len(oldPVs) > 0 {
				example = fmt.Sprintf(", first one is %+v", oldPVs[0])
			}
			return fmt.Errorf("timeout while waiting for %d PVs with selector '%s' - only %d found provisioned%s",
				options.DesiredPVCount, options.filter(), pvStatus.Bound+pvStatus.Available, example)
		case <-time.After(options.WaitForPVsInterval):
			pvs := ps.List()
			pvStatus = ComputePVsStartupStatus(pvs, options.DesiredPVCount)

			diff := DiffPVs(oldPVs, pvs)
			deletedPVs := diff.DeletedPVs()
			if scaling != down && len(deletedPVs) > 0 {
				klog.Errorf("%s: %s: %d PVs disappeared: %v", options.CallerName, options.Selector.String(), len(deletedPVs), strings.Join(deletedPVs, ", "))
			}
			addedPVs := diff.AddedPVs()
			if scaling != up && len(addedPVs) > 0 {
				klog.Errorf("%s: %s: %d PVs appeared: %v", options.CallerName, options.filter(), len(addedPVs), strings.Join(addedPVs, ", "))
			}
			klog.V(2).Infof("%s: %s: %s", options.CallerName, options.filter(), pvStatus.String())
			// We wait until there is a desired number of PVs provisioned and all other PVs are pending.
			if len(pvs) == (pvStatus.Bound+pvStatus.Available+pvStatus.Pending) && pvStatus.Bound+pvStatus.Available == options.DesiredPVCount {
				return nil
			}
			oldPVs = pvs
		}
	}
}
