/*
Copyright 2026 The Kubernetes Authors.

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
	"fmt"
	"math"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	nodeHealthTrackerMeasurementName = "NodeHealthTracker"
	nodeHealthTrackerInformerTimeout = time.Minute
)

func init() {
	if err := measurement.Register(nodeHealthTrackerMeasurementName, createNodeHealthTrackerMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", nodeHealthTrackerMeasurementName, err)
	}
}

func createNodeHealthTrackerMeasurement() measurement.Measurement {
	return &nodeHealthTrackerMeasurement{}
}

type nodeHealthTrackerMeasurement struct {
	isRunning    bool
	stopCh       chan struct{}
	lock         sync.Mutex
	nodes        map[string]bool
	runningNodes int
	nodeCount    int
	hasSynced    bool
	lastLogTime  time.Time
}

func (m *nodeHealthTrackerMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, fmt.Errorf("problem with getting action param: %w", err)
	}

	switch action {
	case "start":
		if err := m.start(config); err != nil {
			return nil, fmt.Errorf("starting NodeHealthTracker measurement problem: %w", err)
		}
		return nil, nil
	case "gather":
		return m.gather(config)
	case "stop":
		m.stop()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (m *nodeHealthTrackerMeasurement) Dispose() {
	m.stop()
}

func (m *nodeHealthTrackerMeasurement) String() string {
	return nodeHealthTrackerMeasurementName
}

func (m *nodeHealthTrackerMeasurement) start(config *measurement.Config) error {
	m.lock.Lock()
	if m.isRunning {
		m.lock.Unlock()
		klog.V(2).Infof("%s: measurement already running", m)
		return nil
	}
	m.isRunning = true
	m.stopCh = make(chan struct{})
	m.nodes = make(map[string]bool)
	m.runningNodes = 0
	m.nodeCount = 0
	m.hasSynced = false
	m.lastLogTime = time.Time{}
	m.lock.Unlock()

	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return err
	}

	ctx := context.Background()
	client := config.ClusterFramework.GetClientSets().GetClient()

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return client.CoreV1().Nodes().List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector.LabelSelector
			options.FieldSelector = selector.FieldSelector
			return client.CoreV1().Nodes().Watch(ctx, options)
		},
	}

	i := informer.NewInformer(lw, m.handleNodeEvent)
	if err := informer.StartAndSync(i, m.stopCh, nodeHealthTrackerInformerTimeout); err != nil {
		return fmt.Errorf("problem starting node health tracker informer: %w", err)
	}

	m.lock.Lock()
	m.hasSynced = true
	m.checkThresholdAndLog()
	m.lock.Unlock()

	return nil
}

func (m *nodeHealthTrackerMeasurement) handleNodeEvent(oldObj, newObj interface{}) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if newObj != nil {
		node, ok := newObj.(*corev1.Node)
		if ok {
			m.nodes[node.Name] = util.IsNodeSchedulableAndUntainted(node)
		}
	} else if oldObj != nil {
		node, ok := oldObj.(*corev1.Node)
		if ok {
			delete(m.nodes, node.Name)
		}
	}

	runningNodes := 0
	for _, healthy := range m.nodes {
		if healthy {
			runningNodes++
		}
	}
	m.runningNodes = runningNodes
	m.nodeCount = len(m.nodes)

	if m.hasSynced {
		m.checkThresholdAndLog()
	}
}

func (m *nodeHealthTrackerMeasurement) checkThresholdAndLog() {
	if m.nodeCount == 0 {
		return
	}
	unhealthyNodes := m.nodeCount - m.runningNodes
	threshold := math.Max(4, float64(m.nodeCount)*0.01)
	if float64(unhealthyNodes) > threshold {
		now := time.Now()
		if m.lastLogTime.IsZero() || now.Sub(m.lastLogTime) >= time.Minute {
			exampleUnhealthyNode := ""
			for nodeName, healthy := range m.nodes {
				if !healthy {
					exampleUnhealthyNode = nodeName
					break
				}
			}
			klog.Warningf("%s: number of unhealthy nodes (%d) is above threshold (%v), total nodes: %d, example unhealthy node: %s", m.String(), unhealthyNodes, threshold, m.nodeCount, exampleUnhealthyNode)
			m.lastLogTime = now
		}
	}
}

func (m *nodeHealthTrackerMeasurement) gather(config *measurement.Config) ([]measurement.Summary, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	if !m.isRunning {
		return nil, fmt.Errorf("measurement %s has not been started", nodeHealthTrackerMeasurementName)
	}

	m.stopLocked()

	summaryData := map[string]int{
		"runningNodes": m.runningNodes,
		"nodeCount":    m.nodeCount,
	}
	content, err := util.PrettyPrintJSON(summaryData)
	if err != nil {
		return nil, fmt.Errorf("pretty print JSON problem: %w", err)
	}

	summary := measurement.CreateSummary(nodeHealthTrackerMeasurementName, "json", content)
	return []measurement.Summary{summary}, nil
}

func (m *nodeHealthTrackerMeasurement) stop() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.stopLocked()
}

func (m *nodeHealthTrackerMeasurement) stopLocked() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
	}
}
