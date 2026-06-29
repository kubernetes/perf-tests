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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func TestNodeHealthTrackerMeasurement_HandleNodeEvent(t *testing.T) {
	m := &nodeHealthTrackerMeasurement{
		isRunning: true,
		stopCh:    make(chan struct{}),
		nodes:     make(map[string]bool),
		hasSynced: true,
		threshold: defaultNodeHealthTrackerThreshold,
		ratio:     defaultNodeHealthTrackerRatio,
	}

	healthyNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}

	unhealthyNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}

	// Add healthy node
	m.handleNodeEvent(nil, healthyNode)
	assert.Equal(t, 1, m.nodeCount)
	assert.Equal(t, 1, m.runningNodes)

	// Add unhealthy node
	m.handleNodeEvent(nil, unhealthyNode)
	assert.Equal(t, 2, m.nodeCount)
	assert.Equal(t, 1, m.runningNodes)

	// Update unhealthy node to healthy
	unhealthyNode.Status.Conditions[0].Status = corev1.ConditionTrue
	m.handleNodeEvent(unhealthyNode, unhealthyNode)
	assert.Equal(t, 2, m.nodeCount)
	assert.Equal(t, 2, m.runningNodes)

	// Delete a node
	m.handleNodeEvent(healthyNode, nil)
	assert.Equal(t, 1, m.nodeCount)
	assert.Equal(t, 1, m.runningNodes)
}

func TestNodeHealthTrackerMeasurement_Debounce(t *testing.T) {
	m := &nodeHealthTrackerMeasurement{
		isRunning: true,
		stopCh:    make(chan struct{}),
		nodes:     make(map[string]bool),
		hasSynced: true,
		threshold: defaultNodeHealthTrackerThreshold,
		ratio:     defaultNodeHealthTrackerRatio,
	}

	// Add 5 unhealthy nodes so unhealthyNodes (5) > max(4, 1% of 5 = 0.05)
	for i := 1; i <= 5; i++ {
		unhealthyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i)},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		}
		m.handleNodeEvent(nil, unhealthyNode)
	}

	firstLogTime := m.lastLogTime
	assert.False(t, firstLogTime.IsZero())

	// Another unhealthy event immediately after should not update lastLogTime due to debounce
	unhealthyNode6 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-6"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}
	m.handleNodeEvent(nil, unhealthyNode6)
	assert.Equal(t, firstLogTime, m.lastLogTime)
}

func TestNodeHealthTrackerMeasurement_Gather(t *testing.T) {
	m := &nodeHealthTrackerMeasurement{
		isRunning:    true,
		stopCh:       make(chan struct{}),
		nodes:        make(map[string]bool),
		runningNodes: 5,
		nodeCount:    10,
		threshold:    defaultNodeHealthTrackerThreshold,
		ratio:        defaultNodeHealthTrackerRatio,
	}

	config := &measurement.Config{
		Params: map[string]interface{}{},
	}
	summaries, err := m.gather(config)
	assert.NoError(t, err)
	assert.Len(t, summaries, 1)
	assert.Equal(t, nodeHealthTrackerMeasurementName, summaries[0].SummaryName())
	assert.False(t, m.isRunning)
}

func TestNodeHealthTrackerMeasurement_Gather_Failure(t *testing.T) {
	m := &nodeHealthTrackerMeasurement{
		isRunning:        true,
		stopCh:           make(chan struct{}),
		nodes:            make(map[string]bool),
		runningNodes:     5,
		nodeCount:        10,
		thresholdReached: true,
		violationMsg:     "number of unhealthy nodes (5) is above threshold (4), total nodes: 10",
		threshold:        defaultNodeHealthTrackerThreshold,
		ratio:            defaultNodeHealthTrackerRatio,
	}

	config := &measurement.Config{
		Params: map[string]interface{}{},
	}
	summaries, err := m.gather(config)
	assert.Error(t, err)
	assert.True(t, errors.IsMetricViolationError(err))
	assert.Len(t, summaries, 1)
}

func TestNodeHealthTrackerMeasurement_ConfigurableParameters(t *testing.T) {
	m := &nodeHealthTrackerMeasurement{
		isRunning: true,
		stopCh:    make(chan struct{}),
		nodes:     make(map[string]bool),
		hasSynced: true,
		threshold: 10,
		ratio:     0.05,
	}

	// Add 5 unhealthy nodes. With threshold=10, max(10, 5*0.05)=10. 5 <= 10, so should not trigger threshold.
	for i := 1; i <= 5; i++ {
		unhealthyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i)},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		}
		m.handleNodeEvent(nil, unhealthyNode)
	}

	assert.False(t, m.thresholdReached)

	// Add 6 more unhealthy nodes (total 11 unhealthy nodes). 11 > 10, so should trigger threshold.
	for i := 6; i <= 11; i++ {
		unhealthyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i)},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		}
		m.handleNodeEvent(nil, unhealthyNode)
	}

	assert.True(t, m.thresholdReached)
}
