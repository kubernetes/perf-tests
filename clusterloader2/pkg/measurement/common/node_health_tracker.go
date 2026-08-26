/*
Copyright The Kubernetes Authors.

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
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	nodeHealthTrackerMeasurementName  = "NodeHealthTracker"
	defaultNodeHealthTrackerThreshold = 4
	defaultNodeHealthTrackerRatio     = 0.01
)

func init() {
	if err := measurement.Register(nodeHealthTrackerMeasurementName, createNodeHealthTrackerMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", nodeHealthTrackerMeasurementName, err)
	}
}

func createNodeHealthTrackerMeasurement() measurement.Measurement {
	return &nodeHealthTrackerMeasurement{
		threshold: defaultNodeHealthTrackerThreshold,
		ratio:     defaultNodeHealthTrackerRatio,
	}
}

type nodeHealthTrackerMeasurement struct {
	isRunning        bool
	stopCh           chan struct{}
	lock             sync.Mutex
	nodes            map[string]bool
	runningNodes     int
	nodeCount        int
	hasSynced        bool
	lastLogTime      time.Time
	thresholdReached bool
	violationMsg     string
	threshold        int
	ratio            float64
	labelSelector    labels.Selector
	fieldSelector    fields.Selector
	nodeInformer     coreinformers.NodeInformer
	registration     cache.ResourceEventHandlerRegistration
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
	threshold, err := util.GetIntOrDefault(config.Params, "threshold", defaultNodeHealthTrackerThreshold)
	if err != nil {
		return fmt.Errorf("problem with getting threshold param: %w", err)
	}

	ratio, err := util.GetFloat64OrDefault(config.Params, "ratio", defaultNodeHealthTrackerRatio)
	if err != nil {
		return fmt.Errorf("problem with getting ratio param: %w", err)
	}

	selector := util.NewObjectSelector()
	if err := selector.Parse(config.Params); err != nil {
		return err
	}

	var labelSelector labels.Selector = labels.Everything()
	if selector.LabelSelector != "" {
		var err error
		labelSelector, err = labels.Parse(selector.LabelSelector)
		if err != nil {
			return fmt.Errorf("failed to parse label selector: %w", err)
		}
	}

	var fieldSelector fields.Selector = fields.Everything()
	if selector.FieldSelector != "" {
		var err error
		fieldSelector, err = fields.ParseSelector(selector.FieldSelector)
		if err != nil {
			return fmt.Errorf("failed to parse field selector: %w", err)
		}
	}

	client := config.ClusterFramework.GetClientSets().GetClient()
	nodeInformer, err := measurementutil.NodeIndexerFactory.NodeInformer(client)
	if err != nil {
		return fmt.Errorf("problem getting shared node informer: %w", err)
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	klog.V(2).Infof("%s: starting node health tracker measurement...", config.Identifier)
	if m.isRunning {
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
	m.thresholdReached = false
	m.violationMsg = ""
	m.threshold = threshold
	m.ratio = ratio
	m.nodeInformer = nodeInformer
	m.labelSelector = labelSelector
	m.fieldSelector = fieldSelector

	for _, obj := range nodeInformer.Informer().GetIndexer().List() {
		node, ok := obj.(*corev1.Node)
		if ok && m.matchesSelector(node) {
			healthy := util.IsNodeSchedulableAndUntainted(node)
			m.nodes[node.Name] = healthy
			if healthy {
				m.runningNodes++
			}
			m.nodeCount++
		}
	}
	m.hasSynced = true
	m.checkThresholdAndLog()

	reg, err := nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			m.handleNodeEvent(nil, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			m.handleNodeEvent(oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				m.handleNodeEvent(tombstone.Obj, nil)
			} else {
				m.handleNodeEvent(obj, nil)
			}
		},
	})
	if err != nil {
		m.isRunning = false
		close(m.stopCh)
		return fmt.Errorf("cannot add event handler: %w", err)
	}
	m.registration = reg

	return nil
}

func (m *nodeHealthTrackerMeasurement) matchesSelector(node *corev1.Node) bool {
	if node == nil {
		return false
	}

	if m.labelSelector != nil && !m.labelSelector.Matches(labels.Set(node.Labels)) {
		return false
	}

	if m.fieldSelector != nil && !m.fieldSelector.Empty() {
		nodeFields := fields.Set{
			"metadata.name":      node.Name,
			"spec.unschedulable": strconv.FormatBool(node.Spec.Unschedulable),
		}
		if !m.fieldSelector.Matches(nodeFields) {
			return false
		}
	}

	return true
}

func (m *nodeHealthTrackerMeasurement) handleNodeEvent(oldObj, newObj interface{}) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.isRunning {
		return
	}

	if newObj != nil {
		node, ok := newObj.(*corev1.Node)
		if !ok {
			return
		}

		matches := m.matchesSelector(node)
		oldHealthy, hadOld := m.nodes[node.Name]

		if matches {
			newHealthy := util.IsNodeSchedulableAndUntainted(node)
			m.nodes[node.Name] = newHealthy

			if !hadOld {
				m.nodeCount++
				if newHealthy {
					m.runningNodes++
				}
			} else if oldHealthy != newHealthy {
				if newHealthy {
					m.runningNodes++
				} else {
					m.runningNodes--
				}
			}
		} else if hadOld {
			delete(m.nodes, node.Name)
			m.nodeCount--
			if oldHealthy {
				m.runningNodes--
			}
		}
	} else if oldObj != nil {
		node, ok := oldObj.(*corev1.Node)
		if !ok {
			return
		}

		if oldHealthy, hadOld := m.nodes[node.Name]; hadOld {
			delete(m.nodes, node.Name)
			m.nodeCount--
			if oldHealthy {
				m.runningNodes--
			}
		}
	}

	if m.hasSynced {
		m.checkThresholdAndLog()
	}
}

func (m *nodeHealthTrackerMeasurement) checkThresholdAndLog() {
	if m.nodeCount == 0 {
		return
	}
	unhealthyNodes := m.nodeCount - m.runningNodes
	threshold := math.Max(float64(m.threshold), float64(m.nodeCount)*m.ratio)
	if float64(unhealthyNodes) > threshold {
		m.thresholdReached = true
		now := time.Now()
		if m.lastLogTime.IsZero() || now.Sub(m.lastLogTime) >= time.Minute {
			exampleUnhealthyNode := ""
			for nodeName, healthy := range m.nodes {
				if !healthy {
					exampleUnhealthyNode = nodeName
					break
				}
			}
			msg := fmt.Sprintf("number of unhealthy nodes (%d) is above threshold (%v), total nodes: %d, example unhealthy node: %s", unhealthyNodes, threshold, m.nodeCount, exampleUnhealthyNode)
			m.violationMsg = msg
			klog.Warningf("%s: %s", m.String(), msg)
			m.lastLogTime = now
		} else if m.violationMsg == "" {
			m.violationMsg = fmt.Sprintf("number of unhealthy nodes (%d) is above threshold (%v), total nodes: %d", unhealthyNodes, threshold, m.nodeCount)
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

	summaryData := map[string]interface{}{
		"runningNodes":     m.runningNodes,
		"nodeCount":        m.nodeCount,
		"thresholdReached": m.thresholdReached,
	}
	content, err := util.PrettyPrintJSON(summaryData)
	if err != nil {
		return nil, fmt.Errorf("pretty print JSON problem: %w", err)
	}

	summary := measurement.CreateSummary(nodeHealthTrackerMeasurementName, "json", content)
	if m.thresholdReached {
		err = errors.NewMetricViolationError("node health", m.violationMsg)
	}
	return []measurement.Summary{summary}, err
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
		if m.registration != nil && m.nodeInformer != nil {
			if err := m.nodeInformer.Informer().RemoveEventHandler(m.registration); err != nil {
				klog.Warningf("%s: failed to remove event handler: %v", m.String(), err)
			}
			m.registration = nil
		}
	}
}
