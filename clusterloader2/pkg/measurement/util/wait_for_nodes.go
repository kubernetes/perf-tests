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
	"time"

	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// WaitForNodeOptions is an options object used by WaitForNodes methods.
type WaitForNodeOptions struct {
	Selector             *util.ObjectSelector
	MinDesiredNodeCount  int
	MaxDesiredNodeCount  int
	CallerName           string
	WaitForNodesInterval time.Duration
}

// WaitForNodes waits till the desired number of nodes is ready.
// If stopCh is closed before all nodes are ready, the error will be returned.
func WaitForNodes(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForNodeOptions) error {
	ps, err := NewNodeStore(clientSet, options.Selector)
	if err != nil {
		return fmt.Errorf("node store creation error: %v", err)
	}
	defer ps.Stop()

	nodeCount := getNumReadyNodes(ps.List())
	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for [%d-%d] Nodes with selector '%v' to be ready - currently there is %d Nodes",
				options.MinDesiredNodeCount, options.MaxDesiredNodeCount, options.Selector.String(), nodeCount)
		case <-time.After(options.WaitForNodesInterval):
			nodeCount = getNumReadyNodes(ps.List())
			klog.V(2).Infof("%s: node count (selector = %v): %d", options.CallerName, options.Selector.String(), nodeCount)
			if options.MinDesiredNodeCount <= nodeCount && nodeCount <= options.MaxDesiredNodeCount {
				return nil
			}
		}
	}
}

func getNumReadyNodes(nodes []*v1.Node) int {
	nReady := 0
	for _, n := range nodes {
		if util.IsNodeSchedulableAndUntainted(n) {
			nReady++
		}
	}
	return nReady
}
