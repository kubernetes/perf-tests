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
	"math/rand"
	"os/exec"
	"sync"
	"time"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

// NodeKiller is a utility to simulate node failures.
type NodeKiller struct {
	config   api.NodeFailureConfig
	client   clientset.Interface
	provider string
}

// NewNodeKiller creates new NodeKiller.
func NewNodeKiller(config api.NodeFailureConfig, client clientset.Interface, provider string) (*NodeKiller, error) {
	if provider != "gce" && provider != "gke" {
		return nil, fmt.Errorf("provider %q is not supported by NodeKiller")
	}
	return &NodeKiller{config, client, provider}, nil
}

// Run starts NodeKiller until stopCh is closed.
func (k *NodeKiller) Run(stopCh <-chan struct{}) {
	// wait.JitterUntil starts work immediately, so wait first.
	time.Sleep(wait.Jitter(time.Duration(k.config.Interval), k.config.JitterFactor))
	wait.JitterUntil(func() {
		nodes, err := k.pickNodes()
		if err != nil {
			glog.Errorf("Unable to pick nodes to kill: %v", err)
			return
		}
		k.kill(nodes)
	}, time.Duration(k.config.Interval), k.config.JitterFactor, true, stopCh)
}

func (k *NodeKiller) pickNodes() ([]v1.Node, error) {
	nodes, err := util.GetSchedulableUntainedNodes(k.client)
	if err != nil {
		return nil, err
	}
	rand.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
	numNodes := int(k.config.FailureRate * float64(len(nodes)))
	if len(nodes) > numNodes {
		return nodes[:numNodes], nil
	}
	return nodes, nil
}

func (k *NodeKiller) kill(nodes []v1.Node) {
	wg := sync.WaitGroup{}
	wg.Add(len(nodes))
	for _, node := range nodes {
		node := node
		go func() {
			defer wg.Done()

			glog.Infof("Stopping docker and kubelet on %q to simulate failure", node.Name)
			err := ssh("sudo systemctl stop docker kubelet", &node)
			if err != nil {
				glog.Errorf("ERROR while stopping node %q: %v", node.Name, err)
				return
			}

			time.Sleep(time.Duration(k.config.SimulatedDowntime))

			glog.Infof("Rebooting %q to repair the node", node.Name)
			err = ssh("sudo reboot", &node)
			if err != nil {
				glog.Errorf("Error while rebooting node %q: %v", node.Name, err)
				return
			}
		}()
	}
	wg.Wait()
}

func ssh(command string, node *v1.Node) error {
	zone, ok := node.Labels["failure-domain.beta.kubernetes.io/zone"]
	if !ok {
		return fmt.Errorf("unknown zone for %q node: no failure-domain.beta.kubernetes.io/zone label", node.Name)
	}
	cmd := exec.Command("gcloud", "compute", "ssh", "--zone", zone, "--command", command, node.Name)
	output, err := cmd.CombinedOutput()
	glog.Infof("ssh to %q finished with %q: %v", node.Name, string(output), err)
	return err
}
