/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/klog"
)

// Transition describe transition between two phases.
type Transition struct {
	From      string
	To        string
	Threshold time.Duration
}

// ObjectTransitionTimes stores beginning time of each phase.
// It can calculate transition latency between phases.
// ObjectTransitionTimes is thread-safe.
type ObjectTransitionTimes struct {
	name string
	lock sync.Mutex
	// times is a map: object key->phase->time.
	times map[string]map[string]time.Time
}

// NewObjectTransitionTimes creates new ObjectTransitionTimes instance.
func NewObjectTransitionTimes(name string) *ObjectTransitionTimes {
	return &ObjectTransitionTimes{
		name:  name,
		times: make(map[string]map[string]time.Time),
	}
}

// Set sets time of given phase for given key.
func (o *ObjectTransitionTimes) Set(key, phase string, t time.Time) {
	o.lock.Lock()
	defer o.lock.Unlock()
	if _, exists := o.times[key]; !exists {
		o.times[key] = make(map[string]time.Time)
	}
	o.times[key][phase] = t
}

// Get returns time of given phase for given key.
func (o *ObjectTransitionTimes) Get(key, phase string) (time.Time, bool) {
	o.lock.Lock()
	defer o.lock.Unlock()
	if entry, exists := o.times[key]; exists {
		val, ok := entry[phase]
		return val, ok
	}
	return time.Time{}, false
}

// Count returns number of key having given phase entry.
func (o *ObjectTransitionTimes) Count(phase string) int {
	o.lock.Lock()
	defer o.lock.Unlock()
	count := 0
	for _, entry := range o.times {
		if _, exists := entry[phase]; exists {
			count++
		}
	}
	return count
}

// KeyFilterFunc is a function that for a given key returns whether
// its corresponding entry should be included in the metric.
type KeyFilterFunc func(string) bool

// MatchAll implements KeyFilterFunc and matches every element.
func MatchAll(_ string) bool { return true }

// CalculateTransitionsLatency returns a latency map for given transitions.
func (o *ObjectTransitionTimes) CalculateTransitionsLatency(t map[string]Transition, filter KeyFilterFunc) map[string]*LatencyMetric {
	o.lock.Lock()
	defer o.lock.Unlock()
	metric := make(map[string]*LatencyMetric)
	for name, transition := range t {
		lag := make([]LatencyData, 0, len(o.times))
		for key, transitionTimes := range o.times {
			if !filter(key) {
				klog.V(4).Infof("%s: filter doesn match key %s", o.name, key)
				continue
			}
			fromPhaseTime, exists := transitionTimes[transition.From]
			if !exists {
				klog.V(4).Infof("%s: failed to find %v time for %v", o.name, transition.From, key)
				continue
			}
			toPhaseTime, exists := transitionTimes[transition.To]
			if !exists {
				klog.V(4).Infof("%s: failed to find %v time for %v", o.name, transition.To, key)
				continue
			}
			latencyTime := toPhaseTime.Sub(fromPhaseTime)
			// latencyTime should be always larger than zero, however, in some cases, it might be a
			// negative value due to the precision of timestamp can only get to the level of second,
			// the microsecond and nanosecond have been discarded purposely in kubelet, this is
			// because apiserver does not support RFC339NANO.
			if latencyTime < 0 {
				latencyTime = 0
			}
			lag = append(lag, latencyData{key: key, latency: latencyTime})
		}

		sort.Sort(LatencySlice(lag))
		o.printLatencies(lag, fmt.Sprintf("worst %s latencies", name), transition.Threshold)
		lagMetric := NewLatencyMetric(lag)
		metric[name] = &lagMetric
	}
	return metric
}

func (o *ObjectTransitionTimes) printLatencies(latencies []LatencyData, header string, threshold time.Duration) {
	metrics := NewLatencyMetric(latencies)
	index := len(latencies) - 100
	if index < 0 {
		index = 0
	}
	klog.V(2).Infof("%s: %d %s: %v", o.name, len(latencies)-index, header, latencies[index:])
	var thresholdString string
	if threshold != time.Duration(0) {
		thresholdString = fmt.Sprintf("; threshold %v", threshold)
	}
	klog.V(0).Infof("%s: perc50: %v, perc90: %v, perc99: %v%s", o.name, metrics.Perc50, metrics.Perc90, metrics.Perc99, thresholdString)
}

type latencyData struct {
	key     string
	latency time.Duration
}

func (l latencyData) GetLatency() time.Duration {
	return l.latency
}

func (l latencyData) String() string {
	return fmt.Sprintf("{%s %v}", l.key, l.latency)
}

// LatencyMapToPerfData converts latency map into PerfData.
func LatencyMapToPerfData(latency map[string]*LatencyMetric) *PerfData {
	perfData := &PerfData{Version: "1.0"}
	for name, l := range latency {
		perfData.DataItems = append(perfData.DataItems, l.ToPerfData(name))
	}
	return perfData
}
