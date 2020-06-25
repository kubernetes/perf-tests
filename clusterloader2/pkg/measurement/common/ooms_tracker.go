/*
Copyright 2020 The Kubernetes Authors.

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
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	clusterOOMsTrackerEnabledParamName   = "clusterOOMsTrackerEnabled"
	clusterOOMsTrackerName               = "ClusterOOMsTracker"
	clusterOOMsIgnoredProcessesParamName = "clusterOOMsIgnoredProcesses"
	informerTimeout                      = time.Minute
	oomEventReason                       = "OOMKilling"
)

var (
	oomEventMsgRegex = regexp.MustCompile(`Kill process (\d+) \((.+)\) score \d+ or sacrifice child\nKilled process \d+ .+ total-vm:(\d+kB), anon-rss:\d+kB, file-rss:\d+kB.*`)
)

func init() {
	if err := measurement.Register(clusterOOMsTrackerName, createClusterOOMsTrackerMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", clusterOOMsTrackerName, err)
	}
}

func createClusterOOMsTrackerMeasurement() measurement.Measurement {
	return &clusterOOMsTrackerMeasurement{}
}

type clusterOOMsTrackerMeasurement struct {
	msgRegex       *regexp.Regexp
	isRunning      bool
	startTime      time.Time
	stopCh         chan struct{}
	processIgnored map[string]bool
	oomsLock       sync.Mutex
	ooms           []oomEvent
}

// TODO: Reevaluate if we can add new fields here when node-problem-detector
// starts using new events.
type oomEvent struct {
	Node          string    `json:"node"`
	Process       string    `json:"process"`
	ProcessMemory string    `json:"memory"`
	ProcessID     string    `json:"pid"`
	Time          time.Time `json:"time"`
}

func (m *clusterOOMsTrackerMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	clusterOOMsTrackerEnabled, err := util.GetBoolOrDefault(config.Params, clusterOOMsTrackerEnabledParamName, false)
	if err != nil {
		return nil, fmt.Errorf("problem with getting %s param: %w", clusterOOMsTrackerEnabledParamName, err)
	}
	if !clusterOOMsTrackerEnabled {
		klog.Info("skipping tracking of OOMs in the cluster")
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, fmt.Errorf("problem with getting %s param: %w", "action", err)
	}

	switch action {
	case "start":
		if err = m.start(config); err != nil {
			return nil, fmt.Errorf("starting cluster OOMs measurement problem: %w", err)
		}
		return nil, nil
	case "gather":
		m.oomsLock.Lock()
		defer m.oomsLock.Unlock()
		return m.gather()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (m *clusterOOMsTrackerMeasurement) Dispose() {
	m.stop()
}

func (m *clusterOOMsTrackerMeasurement) String() string {
	return clusterOOMsTrackerName
}

func (m *clusterOOMsTrackerMeasurement) start(config *measurement.MeasurementConfig) error {
	if m.isRunning {
		klog.Infof("%s: cluster OOMs tracking measurement already running", m)
		return nil
	}
	klog.Infof("%s: starting cluster OOMs tracking measurement...", m)
	if err := m.initFields(config); err != nil {
		return fmt.Errorf("problem with OOMs tracking measurement fields initialization: %w", err)
	}
	// Watching for OOM events from node-problem-detector below.
	i := informer.NewInformer(
		config.ClusterFramework.GetClientSets().GetClient(),
		"events",
		metav1.NamespaceAll,
		fields.Set{"reason": oomEventReason}.AsSelector().String(),
		"",
		m.handleOOMEvent,
	)

	go i.Run(m.stopCh)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, i.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}
	return nil
}

func (m *clusterOOMsTrackerMeasurement) initFields(config *measurement.MeasurementConfig) error {
	m.isRunning = true
	m.startTime = time.Now()
	m.stopCh = make(chan struct{})
	m.msgRegex = oomEventMsgRegex

	ignoredProcessesString, err := util.GetStringOrDefault(config.Params, clusterOOMsIgnoredProcessesParamName, "")
	if err != nil {
		return err
	}
	m.processIgnored = make(map[string]bool)
	if ignoredProcessesString != "" {
		processNames := strings.Split(ignoredProcessesString, ",")
		for _, processName := range processNames {
			m.processIgnored[processName] = true
		}
	}
	return nil
}

func (m *clusterOOMsTrackerMeasurement) stop() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
	}
}

func (m *clusterOOMsTrackerMeasurement) gather() ([]measurement.Summary, error) {
	klog.Infof("%s: gathering cluster OOMs tracking measurement", clusterOOMsTrackerName)
	if !m.isRunning {
		return nil, fmt.Errorf("measurement %s has not been started", clusterOOMsTrackerName)
	}

	m.stop()

	oomData := make(map[string][]oomEvent)
	oomData["failures"] = make([]oomEvent, 0)
	oomData["past"] = make([]oomEvent, 0)
	oomData["ignored"] = make([]oomEvent, 0)

	for _, oom := range m.ooms {
		if m.startTime.After(oom.Time) {
			oomData["past"] = append(oomData["past"], oom)
			continue
		}
		if m.processIgnored[oom.Process] {
			oomData["ignored"] = append(oomData["ignored"], oom)
			continue
		}
		oomData["failures"] = append(oomData["failures"], oom)
	}

	content, err := util.PrettyPrintJSON(oomData)
	if err != nil {
		return nil, fmt.Errorf("OOMs PrettyPrintJSON problem: %w", err)
	}

	summary := measurement.CreateSummary(clusterOOMsTrackerName, "json", content)
	if oomFailures := oomData["failures"]; len(oomFailures) > 0 {
		err = fmt.Errorf("OOMs recorded: %+v", oomFailures)
	}
	return []measurement.Summary{summary}, err
}

func (m *clusterOOMsTrackerMeasurement) handleOOMEvent(_, obj interface{}) {
	if obj == nil {
		return
	}
	event, ok := obj.(*corev1.Event)
	if !ok {
		return
	}

	oom := oomEvent{
		Node: event.InvolvedObject.Name,
	}
	if !event.EventTime.IsZero() {
		oom.Time = event.EventTime.Time
	} else {
		oom.Time = event.FirstTimestamp.Time
	}

	if match := m.msgRegex.FindStringSubmatch(event.Message); len(match) == 4 {
		oom.ProcessID = match[1]
		oom.Process = match[2]
		oom.ProcessMemory = match[3]
	} else {
		klog.Warningf(`unrecognized OOM event message pattern; event message contents: "%v"`, event.Message)
	}

	m.oomsLock.Lock()
	defer m.oomsLock.Unlock()
	m.ooms = append(m.ooms, oom)
}
