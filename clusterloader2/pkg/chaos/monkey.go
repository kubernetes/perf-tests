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

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/api"
)

// Monkey simulates kubernetes component failures
type Monkey struct {
	client     clientset.Interface
	provider   string
	nodeKiller *NodeKiller
}

// NewMonkey constructs a new Monkey object.
func NewMonkey(client clientset.Interface, provider string) *Monkey {
	return &Monkey{client: client, provider: provider}
}

// Init initializes Monkey with given config.
// When stopCh is closed, the Monkey will stop simulating failures.
func (m *Monkey) Init(config api.ChaosMonkeyConfig, stopCh <-chan struct{}) (*sync.WaitGroup, error) {
	wg := sync.WaitGroup{}
	if config.NodeFailure != nil {
		nodeKiller, err := NewNodeKiller(*config.NodeFailure, m.client, m.provider)
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
