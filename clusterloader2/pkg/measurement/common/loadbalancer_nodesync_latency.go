/*
Copyright 2021 The Kubernetes Authors.

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

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	loadBalancerNodeSyncLatencyName = "LoadBalancerNodeSyncLatency"
	defaultNodeSyncLatencyTimeout   = 30 * time.Minute

	// excludeFromLoadBalancersLabel is the node label to exclude a node from being a LB backend
	excludeFromLoadBalancersLabel = "node.kubernetes.io/exclude-from-external-load-balancers"
	// nodeSyncEventReason is the event reason emitted by service controller when it completes node sync on the lb.
	nodeSyncEventReason = "UpdatedLoadBalancer"

	phaseNodeSyncStart    = "nodesync_triggered"
	phaseNodeSyncComplete = "nodesync_complete"
)

var nodeSyncTransition = map[string]measurementutil.Transition{
	"nodesync_start_to_complete": {
		From: phaseNodeSyncStart,
		To:   phaseNodeSyncComplete,
	},
}

func init() {
	if err := measurement.Register(loadBalancerNodeSyncLatencyName, createLoadBalancerNodeSyncMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", loadBalancerNodeSyncLatencyName, err)
	}
}

func createLoadBalancerNodeSyncMeasurement() measurement.Measurement {
	return &LoadBalancerNodeSyncMeasurement{
		selector:                  util.NewObjectSelector(),
		svcNodeSyncLatencyTracker: measurementutil.NewObjectTransitionTimes(loadBalancerNodeSyncLatencyName),
	}
}

type LoadBalancerNodeSyncMeasurement struct {
	client clientset.Interface
	// selector used to select relevant load balancer type service used for measurement
	selector *util.ObjectSelector
	// waitTimeout specify for the timeout for node sync on all LBs to complete
	waitTimeout time.Duration
	// svcNodeSyncLatencyTracker tracks the nodesync latency
	svcNodeSyncLatencyTracker *measurementutil.ObjectTransitionTimes
	// excludedNodeName is the node name used to trigger LB nodesync
	excludedNodeName string
	// lbSvcMap is the map that contains load balancer type service with key (namespaced/name) and service
	lbSvcMap map[string]v1.Service
}

// LoadBalancerNodeSyncMeasurement takes measurement of node sync latency for selected lb type services.
// This measurement only works for K8s 1.19 as it depends on the ExcludeNodeForLoadbalancer label.
// Services can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all LoadBalancer type service with all-namespace scope is assumed.
// "measure" action triggers nodesync and observation of nodesync completion for selected LB services.
// "gather" returns node sync latency summary.
func (s *LoadBalancerNodeSyncMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	s.client = config.ClusterFramework.GetClientSets().GetClient()
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "measure":
		if err := s.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		s.waitTimeout, err = util.GetDurationOrDefault(config.Params, "waitTimeout", defaultNodeSyncLatencyTimeout)
		if err != nil {
			return nil, err
		}
		return nil, s.measureNodeSyncLatency()
	case "gather":
		if err := s.labelNodeForLBs(false); err != nil {
			return nil, err
		}
		return s.gather(config.Identifier)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (s *LoadBalancerNodeSyncMeasurement) Dispose() {}

func (s *LoadBalancerNodeSyncMeasurement) String() string {
	return loadBalancerNodeSyncLatencyName + ": " + s.selector.String()
}

func (s *LoadBalancerNodeSyncMeasurement) measureNodeSyncLatency() error {
	ctx := context.Background()
	options := metav1.ListOptions{}
	s.selector.ApplySelectors(&options)
	svcList, err := s.client.CoreV1().Services(s.selector.Namespace).List(ctx, options)
	if err != nil {
		return err
	}

	s.lbSvcMap = map[string]v1.Service{}
	for _, svc := range svcList.Items {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
			s.lbSvcMap[keyFunc(svc.Namespace, svc.Name)] = svc
		}
	}
	totalLbSvc := len(s.lbSvcMap)

	// Use event informer to keep track of nodeSync events.
	stopCh := make(chan struct{})
	defer close(stopCh)

	eventInformer := s.getEventInformer()
	go eventInformer.Run(stopCh)

	// trigger node sync by picking a node and add exclude lb label
	nodeList, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, node := range nodeList.Items {
		if isCandidateNode(node) {
			s.excludedNodeName = node.Name
			break
		}
	}

	if s.excludedNodeName == "" {
		return fmt.Errorf("failed to find a node candidate to trigger nodesync from node list: %v", nodeList.Items)
	}

	defer func() {
		if err = s.labelNodeForLBs(false); err != nil {
			klog.Errorf("Failed to label node %v: %v", s.excludedNodeName, err)
		}

	}()
	if err = s.labelNodeForLBs(true); err != nil {
		return err
	}

	now := time.Now()
	for key := range s.lbSvcMap {
		s.svcNodeSyncLatencyTracker.Set(key, phaseNodeSyncStart, now)
	}

	return wait.Poll(5*time.Second, s.waitTimeout, func() (done bool, err error) {
		if s.svcNodeSyncLatencyTracker.Count(phaseNodeSyncComplete) == totalLbSvc {
			return true, nil
		}
		klog.V(2).Infof("out of a total of %v LBs, %v LB type service has %q event", totalLbSvc, s.svcNodeSyncLatencyTracker.Count(phaseNodeSyncComplete), nodeSyncEventReason)
		return false, nil
	})
}

func (s *LoadBalancerNodeSyncMeasurement) getEventInformer() cache.Controller {
	ctx := context.Background()
	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		o := metav1.ListOptions{
			Limit: 1,
		}
		result, err := s.client.CoreV1().Events(metav1.NamespaceAll).List(ctx, o)
		if err != nil {
			return nil, err
		}
		result.Continue = ""
		result.Items = nil
		return result, nil
	}

	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		options.FieldSelector = fields.Set{"reason": nodeSyncEventReason}.AsSelector().String()
		return s.client.CoreV1().Events(metav1.NamespaceAll).Watch(ctx, options)
	}

	_, eventInformer := cache.NewInformer(&cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}, nil, 0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				s.processEvent(obj.(*v1.Event))
			},
		})
	return eventInformer
}

func (s *LoadBalancerNodeSyncMeasurement) processEvent(event *v1.Event) {
	if event.Reason != nodeSyncEventReason {
		return
	}

	key := keyFunc(event.InvolvedObject.Namespace, event.InvolvedObject.Name)
	_, ok := s.lbSvcMap[key]
	if ok {
		_, found := s.svcNodeSyncLatencyTracker.Get(key, phaseNodeSyncComplete)
		if !found {
			s.svcNodeSyncLatencyTracker.Set(key, phaseNodeSyncComplete, event.CreationTimestamp.Time)
		}
	}
}

// labelNodeForLBs manipulates candidate node to include or exclude it from being LB backends.
func (s *LoadBalancerNodeSyncMeasurement) labelNodeForLBs(exclude bool) error {
	ctx := context.Background()
	node, err := s.client.CoreV1().Nodes().Get(ctx, s.excludedNodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	newNode := node.DeepCopy()

	if exclude {
		newNode.Labels[excludeFromLoadBalancersLabel] = "true"
	} else {
		delete(newNode.Labels, excludeFromLoadBalancersLabel)
	}

	patchBytes, err := preparePatchBytes(node, newNode, v1.Node{})
	if err != nil {
		return err
	}

	_, err = s.client.CoreV1().Nodes().Patch(ctx, s.excludedNodeName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (s *LoadBalancerNodeSyncMeasurement) gather(identifier string) ([]measurement.Summary, error) {
	klog.V(2).Infof("%s: gathering nodesync latency measurement...", s)
	nodeSyncLatency := s.svcNodeSyncLatencyTracker.CalculateTransitionsLatency(nodeSyncTransition, measurementutil.MatchAll)
	content, err := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(nodeSyncLatency))
	if err != nil {
		return nil, err
	}

	summary := measurement.CreateSummary(fmt.Sprintf("%s_%s", loadBalancerNodeSyncLatencyName, identifier), "json", content)

	// TODO: return an error here if latency is higher than an upper bound.
	return []measurement.Summary{summary}, nil
}

// isCandidateNode returns if node can be used to trigger nodesync
func isCandidateNode(node v1.Node) bool {
	if _, hasExcludeBalancerLabel := node.Labels[excludeFromLoadBalancersLabel]; hasExcludeBalancerLabel {
		return false
	}
	// If we have no info, don't accept
	if len(node.Status.Conditions) == 0 {
		return false
	}
	for _, cond := range node.Status.Conditions {
		// We consider the node for load balancing only when its NodeReady condition status
		// is ConditionTrue
		if cond.Type == v1.NodeReady && cond.Status != v1.ConditionTrue {
			klog.V(4).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
			return false
		}
	}
	return true
}

func preparePatchBytes(old, new, refStruct interface{}) ([]byte, error) {
	oldBytes, err := json.Marshal(old)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %v", err)
	}

	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %v", err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldBytes, newBytes, refStruct)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}
	return patchBytes, nil
}

func keyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
