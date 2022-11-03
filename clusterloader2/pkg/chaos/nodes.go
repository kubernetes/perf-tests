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

package chaos

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

const (
	monitoringNamespace = "monitoring"
	prometheusLabel     = "prometheus=k8s"
)

// NodeKiller is a utility to simulate node failures.
type NodeKiller struct {
	config api.NodeFailureConfig
	client clientset.Interface
	// killedNodes stores names of the nodes that have been killed by NodeKiller.
	killedNodes sets.String
	recorder    *eventRecorder
	ssh         util.SSHExecutor
}

type nodeAction string

const (
	stopServices nodeAction = "stopService"
	rebootNode              = "rebootNode"
)

type event struct {
	time     time.Time
	action   nodeAction
	nodeName string
}

type eventRecorder struct {
	events []event
	mux    sync.Mutex
}

func newEventRecorder() *eventRecorder {
	return &eventRecorder{[]event{}, sync.Mutex{}}
}

func (r *eventRecorder) record(a nodeAction, nodeName string) {
	e := event{time.Now(), a, nodeName}
	r.mux.Lock()
	r.events = append(r.events, e)
	r.mux.Unlock()
}

// NewNodeKiller creates new NodeKiller.
func NewNodeKiller(config api.NodeFailureConfig, client clientset.Interface, killedNodes sets.String, provider provider.Provider) (*NodeKiller, error) {
	// TODO(#1399): node-killing code is provider specific, move it into provider
	if !provider.Features().SupportNodeKiller {
		return nil, fmt.Errorf("provider %q is not supported by NodeKiller", provider)
	}
	sshExecutor := &util.GCloudSSHExecutor{}
	return &NodeKiller{config, client, killedNodes, newEventRecorder(), sshExecutor}, nil
}

// Run starts NodeKiller until stopCh is closed.
func (k *NodeKiller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	// wait.JitterUntil starts work immediately, so wait first.
	sleepInterrupt(wait.Jitter(time.Duration(k.config.Interval), k.config.JitterFactor), stopCh)
	wait.JitterUntil(func() {
		nodes, err := k.pickNodes()
		if err != nil {
			klog.Errorf("%s: Unable to pick nodes to kill: %v", k, err)
			return
		}
		k.kill(nodes, stopCh)
	}, time.Duration(k.config.Interval), k.config.JitterFactor, true, stopCh)
}

func (k *NodeKiller) pickNodes() ([]v1.Node, error) {
	allNodes, err := util.GetSchedulableUntainedNodes(k.client)
	if err != nil {
		return nil, err
	}

	prometheusPods, err := client.ListPodsWithOptions(k.client, monitoringNamespace, metav1.ListOptions{
		LabelSelector: prometheusLabel,
	})
	if err != nil {
		return nil, err
	}
	nodesHasPrometheusPod := sets.NewString()
	for i := range prometheusPods {
		if prometheusPods[i].Spec.NodeName != "" {
			nodesHasPrometheusPod.Insert(prometheusPods[i].Spec.NodeName)
			klog.V(2).Infof("%s: Node %s removed from killing. Runs pod %s", k, prometheusPods[i].Spec.NodeName, prometheusPods[i].Name)
		}
	}

	nodes := allNodes[:0]
	for _, node := range allNodes {
		if !nodesHasPrometheusPod.Has(node.Name) && !k.killedNodes.Has(node.Name) {
			nodes = append(nodes, node)
		}
	}
	rand.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
	numNodes := int(math.Ceil(k.config.FailureRate * float64(len(nodes))))
	klog.V(2).Infof("%s: %d nodes available, wants to fail %d nodes", k, len(nodes), numNodes)
	if len(nodes) > numNodes {
		nodes = nodes[:numNodes]
	}
	for _, node := range nodes {
		klog.V(2).Infof("%s: Node %q schedule for failure", k, node.Name)
	}
	return nodes, nil
}

func (k *NodeKiller) kill(nodes []v1.Node, stopCh <-chan struct{}) {
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))
	for _, node := range nodes {
		k.killedNodes.Insert(node.Name)
		node := node
		go func() {
			defer wg.Done()

			klog.V(2).Infof("%s: Stopping docker and kubelet on %q to simulate failure", k, node.Name)
			k.addStopServicesEvent(node.Name)
			err := k.ssh.Exec("sudo systemctl stop docker kubelet", &node, nil)
			if err != nil {
				klog.Errorf("%s: ERROR while stopping node %q: %v", k, node.Name, err)
				return
			}

			// Listening for interruptions on stopCh or wait for the simulated downtime
			sleepInterrupt(time.Duration(k.config.SimulatedDowntime), stopCh)

			klog.V(2).Infof("%s: Rebooting %q to repair the node", k, node.Name)
			k.addRebootEvent(node.Name)
			err = k.ssh.Exec("sudo reboot", &node, nil)
			if err != nil {
				klog.Errorf("%s: Error while rebooting node %q: %v", k, node.Name, err)
				return
			}
		}()
	}
	wg.Wait()
}

func (k *NodeKiller) addStopServicesEvent(nodeName string) {
	k.recorder.record(stopServices, nodeName)
}

func (k *NodeKiller) addRebootEvent(nodeName string) {
	k.recorder.record(rebootNode, nodeName)
}

// Summary logs NodeKiller execution
func (k *NodeKiller) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s: Recorded following events\n", k))
	for _, e := range k.recorder.events {
		sb.WriteString(fmt.Sprintf("%s: At %v %v happend for node %s\n", k, e.time.Format(time.UnixDate), e.action, e.nodeName))
	}
	return sb.String()
}

func (k *NodeKiller) String() string {
	return "NodeKiller"
}

// Utility method to put the current routine to sleep or break the sleep if stopCh closes
// Note of warning: if stopCh is already closed the process may not sleep at all.
func sleepInterrupt(duration time.Duration, stopCh <-chan struct{}) {
	select {
	case <-stopCh:
		break
	case <-time.After(duration):
		break
	}
}
