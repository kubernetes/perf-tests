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
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// WaitForNodeOptions is an options object used by WaitForNodes methods.
type WaitForNodeOptions struct {
	Selector             *util.ObjectSelector
	MinDesiredNodeCount  int
	MaxDesiredNodeCount  int
	CallerName           string
	WaitForNodesInterval time.Duration
	TolerationTimeout    time.Duration
}

// WaitForNodes waits till the desired number of nodes is ready.
// If stopCh is closed before all nodes are ready, the error will be returned.
func WaitForNodes(clientSet clientset.Interface, stopCh <-chan struct{}, options *WaitForNodeOptions) error {
	nodeIndexer, err := NodeIndexerFactory.NodeIndexer(clientSet)
	if err != nil {
		return fmt.Errorf("node indexer creation error: %w", err)
	}

	nodes, err := filterNodes(nodeIndexer, options.Selector)
	if err != nil {
		return fmt.Errorf("failed to filter nodes: %w", err)
	}

	nodeCount := getNumReadyNodes(nodes)
	if options.MinDesiredNodeCount <= nodeCount && nodeCount <= options.MaxDesiredNodeCount {
		return nil
	}

	var tolerationCh <-chan time.Time
	if options.TolerationTimeout > 0 {
		timer := time.NewTimer(options.TolerationTimeout)
		defer timer.Stop()
		tolerationCh = timer.C
	}

	var tolerationExpired bool
	var tolerationExpiredAt time.Time

	for {
		select {
		case <-stopCh:
			return fmt.Errorf("timeout while waiting for [%d-%d] Nodes with selector '%v' to be ready - currently there is %d Nodes",
				options.MinDesiredNodeCount, options.MaxDesiredNodeCount, options.Selector.String(), nodeCount)
		case <-tolerationCh:
			nodes, err := filterNodes(nodeIndexer, options.Selector)
			if err != nil {
				return fmt.Errorf("failed to filter nodes: %w", err)
			}

			nodeCount = getNumReadyNodes(nodes)
			klog.V(2).Infof("%s: toleration timeout expired, node count (selector = %v): %d", options.CallerName, options.Selector.String(), nodeCount)
			if options.MinDesiredNodeCount <= nodeCount && nodeCount <= options.MaxDesiredNodeCount {
				return nil
			}
			tolerationExpired = true
			tolerationExpiredAt = time.Now()
		case <-time.After(options.WaitForNodesInterval):
			nodes, err := filterNodes(nodeIndexer, options.Selector)
			if err != nil {
				return fmt.Errorf("failed to filter nodes: %w", err)
			}

			nodeCount = getNumReadyNodes(nodes)
			klog.V(2).Infof("%s: node count (selector = %v): %d", options.CallerName, options.Selector.String(), nodeCount)
			if options.MinDesiredNodeCount <= nodeCount && nodeCount <= options.MaxDesiredNodeCount {
				if tolerationExpired {
					delay := time.Since(tolerationExpiredAt)
					return fmt.Errorf("desired number of [%d-%d] Nodes with selector '%v' reached after tolerationTimeout (%v), delay after tolerationTimeout was %v",
						options.MinDesiredNodeCount, options.MaxDesiredNodeCount, options.Selector.String(), options.TolerationTimeout, delay)
				}
				return nil
			}
		}
	}
}

func filterNodes(indexer cache.Indexer, selector *util.ObjectSelector) ([]*v1.Node, error) {
	objects := indexer.List()

	var labelSelector labels.Selector = labels.Everything()
	if selector != nil && selector.LabelSelector != "" {
		var err error
		labelSelector, err = labels.Parse(selector.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to parse label selector: %w", err)
		}
	}

	var fieldSelector fields.Selector = fields.Everything()
	if selector != nil && selector.FieldSelector != "" {
		var err error
		fieldSelector, err = fields.ParseSelector(selector.FieldSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to parse field selector: %w", err)
		}
	}

	nodes := make([]*v1.Node, 0, len(objects))
	for _, obj := range objects {
		node, ok := obj.(*v1.Node)
		if !ok {
			continue
		}

		if !labelSelector.Matches(labels.Set(node.Labels)) {
			continue
		}

		if !fieldSelector.Empty() {
			nodeFields := fields.Set{
				"metadata.name":      node.Name,
				"spec.unschedulable": strconv.FormatBool(node.Spec.Unschedulable),
			}
			if !fieldSelector.Matches(nodeFields) {
				continue
			}
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
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
