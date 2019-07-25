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

package gatherers

import (
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/kubelet"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/kubemark"
)

type resourceGatherWorker struct {
	c                           clientset.Interface
	nodeName                    string
	wg                          *sync.WaitGroup
	containerIDs                []string
	stopCh                      chan struct{}
	dataSeries                  []util.ResourceUsagePerContainer
	finished                    bool
	inKubemark                  bool
	resourceDataGatheringPeriod time.Duration
	printVerboseLogs            bool
	host                        string
	provider                    string
}

func (w *resourceGatherWorker) singleProbe() {
	data := make(util.ResourceUsagePerContainer)
	if w.inKubemark {
		kubemarkData := kubemark.GetKubemarkMasterComponentsResourceUsage(w.host, w.provider)
		if data == nil {
			return
		}
		for k, v := range kubemarkData {
			data[k] = &util.ContainerResourceUsage{
				Name:                    v.Name,
				MemoryWorkingSetInBytes: v.MemoryWorkingSetInBytes,
				CPUUsageInCores:         v.CPUUsageInCores,
			}
		}
	} else {
		nodeUsage, err := kubelet.GetOneTimeResourceUsageOnNode(w.c, w.nodeName, func() []string { return w.containerIDs })
		if err != nil {
			klog.Errorf("error while reading data from %v: %v", w.nodeName, err)
			return
		}
		for k, v := range nodeUsage {
			data[k] = v
			if w.printVerboseLogs {
				klog.Infof("Get container %v usage on node %v. CPUUsageInCores: %v, MemoryUsageInBytes: %v, MemoryWorkingSetInBytes: %v", k, w.nodeName, v.CPUUsageInCores, v.MemoryUsageInBytes, v.MemoryWorkingSetInBytes)
			}
		}
	}
	w.dataSeries = append(w.dataSeries, data)
}

func (w *resourceGatherWorker) gather(initialSleep time.Duration) {
	defer utilruntime.HandleCrash()
	defer w.wg.Done()
	defer klog.Infof("Closing worker for %v", w.nodeName)
	defer func() { w.finished = true }()
	select {
	case <-time.After(initialSleep):
		w.singleProbe()
		for {
			select {
			case <-time.After(w.resourceDataGatheringPeriod):
				w.singleProbe()
			case <-w.stopCh:
				return
			}
		}
	case <-w.stopCh:
		return
	}
}
