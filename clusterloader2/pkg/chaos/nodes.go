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
	"k8s.io/klog"
)

const (
	monitoringNamespace = "monitoring"
	prometheusLabel     = "prometheus=k8s"
)

// NodeKiller is a utility to simulate node failures.
type NodeKiller struct {
	config   api.NodeFailureConfig
	client   clientset.Interface
	provider string
	// killedNodes stores names of the nodes that have been killed by NodeKiller.
	killedNodes sets.String
}

// NewNodeKiller creates new NodeKiller.
func NewNodeKiller(config api.NodeFailureConfig, client clientset.Interface, provider string) (*NodeKiller, error) {
	if provider != "gce" && provider != "gke" {
		return nil, fmt.Errorf("provider %q is not supported by NodeKiller", provider)
	}
	return &NodeKiller{config, client, provider, sets.NewString()}, nil
}

// Run starts NodeKiller until stopCh is closed.
func (k *NodeKiller) Run(stopCh <-chan struct{}) {
	// wait.JitterUntil starts work immediately, so wait first.
	time.Sleep(wait.Jitter(time.Duration(k.config.Interval), k.config.JitterFactor))
	wait.JitterUntil(func() {
		nodes, err := k.pickNodes()
		if err != nil {
			klog.Errorf("%s: Unable to pick nodes to kill: %v", k, err)
			return
		}
		k.kill(nodes)
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
			klog.Infof("%s: Node %s removed from killing. Runs pod %s", k, prometheusPods[i].Spec.NodeName, prometheusPods[i].Name)
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
	klog.Infof("%s: %d nodes available, wants to fail %d nodes", k, len(nodes), numNodes)
	if len(nodes) > numNodes {
		nodes = nodes[:numNodes]
	}
	for _, node := range nodes {
		klog.Infof("%s: Node %q schedule for failure", k, node.Name)
	}
	return nodes, nil
}

func (k *NodeKiller) kill(nodes []v1.Node) {
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))
	for _, node := range nodes {
		k.killedNodes.Insert(node.Name)
		node := node
		go func() {
			defer wg.Done()

			klog.Infof("%s: Stopping docker and kubelet on %q to simulate failure", k, node.Name)
			err := util.SSH("sudo systemctl stop docker kubelet", &node, nil)
			if err != nil {
				klog.Errorf("%s: ERROR while stopping node %q: %v", k, node.Name, err)
				return
			}

			time.Sleep(time.Duration(k.config.SimulatedDowntime))

			klog.Infof("%s: Rebooting %q to repair the node", k, node.Name)
			// Scheduling a reboot in one second, then disconnecting.
			//
			// Bash command explanation:
			// 'nohup' - Making sure that end of SSH connection signal will not break sudo
			// 'sudo' - Elevated priviliages, required by 'shutdown'
			// 'shutdown' - Control machine power
			// '-r' - Making 'shutdown' to reboot, instead of power-off
			// '+1s' - Parameter to 'reboot', to wait 1 second before rebooting.
			// '> /dev/null 2> /dev/null < /dev/null' - File descriptor redirect, all three I/O to avoid ssh hanging,
			//                                          see https://web.archive.org/web/20090429074212/http://www.openssh.com/faq.html#3.10
			// '&' - Execute command in background, end without waiting for result
			err = util.SSH("nohup sudo shutdown -r +1s > /dev/null 2> /dev/null < /dev/null &", &node, nil)
			if err != nil {
				klog.Errorf("%s: Error while rebooting node %q: %v", k, node.Name, err)
				return
			}
		}()
	}
	wg.Wait()
}

func (k *NodeKiller) String() string {
	return "NodeKiller"
}
