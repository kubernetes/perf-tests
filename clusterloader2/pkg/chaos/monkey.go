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
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

// Monkey simulates kubernetes component failures
type Monkey struct {
	client     clientset.Interface
	provider   provider.Provider
	nodeKiller *NodeKiller
}

// NewMonkey constructs a new Monkey object.
func NewMonkey(client clientset.Interface, provider provider.Provider) *Monkey {
	return &Monkey{client: client, provider: provider}
}

// Init initializes Monkey with given config.
// When stopCh is closed, the Monkey will stop simulating failures.
func (m *Monkey) Init(config api.ChaosMonkeyConfig, stopCh <-chan struct{}) (*sync.WaitGroup, error) {
	wg := sync.WaitGroup{}
	if config.NodeFailure != nil {
		nodeKiller, err := NewNodeKiller(*config.NodeFailure, m.client, config.ExcludedNodes, m.provider)
		if err != nil {
			return nil, err
		}
		m.nodeKiller = nodeKiller
		wg.Add(1)
		go m.nodeKiller.Run(stopCh, &wg)
	}

	return &wg, nil
}

// Summary logs Monkey execution
func (m *Monkey) Summary() string {
	var sb strings.Builder
	if m.nodeKiller != nil {
		sb.WriteString(fmt.Sprintf("Summary of Chaos Monkey execution\n"))
		sb.WriteString(m.nodeKiller.Summary())
	}
	return sb.String()
}

// KilledNodes returns the set of nodes which shouldn't be restarted again.
func (m *Monkey) KilledNodes() sets.String {
	if m.nodeKiller != nil {
		return m.nodeKiller.killedNodes
	}
	return sets.NewString()
}
