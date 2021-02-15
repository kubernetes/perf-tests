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
	"strings"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
)

const keyMasterNodeLabel = "node-role.kubernetes.io/master"

// GetSchedulableNodesNumber returns number of nodes in the cluster.
func GetSchedulableNodesNumber(c clientset.Interface) (int, error) {
	nodes, err := GetSchedulableNodes(c)
	return len(nodes), err
}

// GetSchedulableNodes returns nodes in the cluster.
func GetSchedulableNodes(c clientset.Interface) ([]corev1.Node, error) {
	nodeList, err := client.ListNodes(c)
	if err != nil {
		return nil, err
	}
	var filtered []corev1.Node
	for i := range nodeList {
		if IsNodeSchedulable(&nodeList[i]) {
			filtered = append(filtered, nodeList[i])
		}
	}
	return filtered, err
}

// LogClusterNodes prints nodes information (name, internal ip, external ip) to log.
func LogClusterNodes(c clientset.Interface) error {
	nodeList, err := client.ListNodes(c)
	if err != nil {
		return err
	}
	klog.V(2).Infof("Listing cluster nodes:")
	for i := range nodeList {
		var internalIP, externalIP string
		isSchedulable := IsNodeSchedulable(&nodeList[i])
		for _, address := range nodeList[i].Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				internalIP = address.Address
			}
			if address.Type == corev1.NodeExternalIP {
				externalIP = address.Address
			}
		}
		klog.V(2).Infof("Name: %v, clusterIP: %v, externalIP: %v, isSchedulable: %v", nodeList[i].ObjectMeta.Name, internalIP, externalIP, isSchedulable)
	}
	return nil
}

// IsNodeSchedulable returns true for a particular node iff:
// 1) it doesn't have "unschedulable" field set,
// 2) its Ready condition is set to true,
// 3) it doesn't have NetworkUnavailable condition set to true.
func IsNodeSchedulable(node *corev1.Node) bool {
	nodeReady := isNodeConditionSetAsExpected(node, corev1.NodeReady, true)
	networkReady := isNodeConditionUnset(node, corev1.NodeNetworkUnavailable) ||
		isNodeConditionSetAsExpected(node, corev1.NodeNetworkUnavailable, false)
	return !node.Spec.Unschedulable && nodeReady && networkReady
}

func isNodeConditionSetAsExpected(node *corev1.Node, conditionType corev1.NodeConditionType, wantTrue bool) bool {
	// Check the node readiness condition (logging all).
	for _, cond := range node.Status.Conditions {
		// Ensure that the condition type and the status matches as desired.
		if cond.Type == conditionType {
			if wantTrue == (cond.Status == corev1.ConditionTrue) {
				return true
			}
			klog.V(4).Infof("Condition %s of node %s is %v instead of %t. Reason: %v, message: %v",
				conditionType, node.Name, cond.Status == corev1.ConditionTrue, wantTrue, cond.Reason, cond.Message)
			return false
		}
	}
	klog.V(4).Infof("Couldn't find condition %v on node %v", conditionType, node.Name)
	return false
}

func isNodeConditionUnset(node *corev1.Node, conditionType corev1.NodeConditionType) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == conditionType {
			return false
		}
	}
	return true
}

// GetMasterName returns master node name.
func GetMasterName(c clientset.Interface) (string, error) {
	nodeList, err := client.ListNodes(c)
	if err != nil {
		return "", err
	}
	for i := range nodeList {
		if LegacyIsMasterNode(&nodeList[i]) {
			return nodeList[i].Name, nil
		}
	}
	return "", fmt.Errorf("master node not found")
}

// GetMasterIPs returns master node ips of the given type.
func GetMasterIPs(c clientset.Interface, addressType corev1.NodeAddressType) ([]string, error) {
	nodeList, err := client.ListNodes(c)
	if err != nil {
		return nil, err
	}
	var ips []string
	for i := range nodeList {
		if LegacyIsMasterNode(&nodeList[i]) {
			for _, address := range nodeList[i].Status.Addresses {
				if address.Type == addressType && address.Address != "" {
					ips = append(ips, address.Address)
					break
				}
			}
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("didn't find any %s master IPs", addressType)
	}
	return ips, nil
}

// LegacyIsMasterNode returns true if given node is a registered master according
// to the logic historically used for this function. This code path is deprecated
// and the node disruption exclusion label should be used in the future.
// This code will not be allowed to update to use the node-role label, since
// node-roles may not be used for feature enablement.
// DEPRECATED: this will be removed in Kubernetes 1.19
func LegacyIsMasterNode(node *corev1.Node) bool {
	for key := range node.GetLabels() {
		if key == keyMasterNodeLabel {
			return true
		}
	}

	// We are trying to capture "master(-...)?$" regexp.
	// However, using regexp.MatchString() results even in more than 35%
	// of all space allocations in ControllerManager spent in this function.
	// That's why we are trying to be a bit smarter.
	nodeName := node.GetName()
	if strings.HasSuffix(nodeName, "master") {
		return true
	}
	if len(nodeName) >= 10 {
		return strings.HasSuffix(nodeName[:len(nodeName)-3], "master-")
	}
	return false
}
