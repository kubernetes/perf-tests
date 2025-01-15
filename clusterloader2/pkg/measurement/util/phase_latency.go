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
	"regexp"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
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

func (o *ObjectTransitionTimes) Exists(key string) bool {
	o.lock.Lock()
	defer o.lock.Unlock()
	_, exists := o.times[key]
	return exists
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

func (o *ObjectTransitionTimes) Keys() <-chan string {
	ch := make(chan string, len(o.times))
	go func() {
		o.lock.Lock()
		defer o.lock.Unlock()
		for key := range o.times {
			ch <- key
		}
		close(ch)
	}()
	return ch
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

type EventTimeAndCount struct {
	firstTimestamp metav1.Time
	lastTimestamp  metav1.Time
	count          int32
}

type PodCreationEventTimes struct {
	lock         sync.Mutex
	events       map[string]map[string]EventTimeAndCount
	podNameRegex *regexp.Regexp
}

func NewPodCreationEventTimes() *PodCreationEventTimes {
	return &PodCreationEventTimes{
		events:       make(map[string]map[string]EventTimeAndCount),
		podNameRegex: regexp.MustCompile(`Created pod: (.*)`),
	}
}

func (pc *PodCreationEventTimes) Set(key string, event *corev1.Event) {
	match := pc.podNameRegex.FindStringSubmatch(event.Message)
	pod_name := match[1]

	pc.lock.Lock()
	defer pc.lock.Unlock()
	if _, exists := pc.events[key]; !exists {
		pc.events[key] = make(map[string]EventTimeAndCount)
	}
	pc.events[key][pod_name] = EventTimeAndCount{
		firstTimestamp: event.FirstTimestamp,
		lastTimestamp:  event.LastTimestamp,
		count:          event.Count,
	}
}

func (pc *PodCreationEventTimes) CalculateLatency(jobCreationTimes map[string]time.Time) LatencyMetric {
	pc.lock.Lock()
	defer pc.lock.Unlock()

	latencies := make([]LatencyData, 0, len(pc.events))
	for job_name, events := range pc.events {
		jobCreateTime, exists := jobCreationTimes[job_name]
		if !exists {
			continue
		}
		for pod_name, event := range events {
			latencies = append(latencies, latencyData{
				key:     pod_name,
				latency: event.firstTimestamp.Sub(jobCreateTime),
			})
			if event.count == 1 {
				continue
			}
			// Assume individual events in aggregated events distribute uniformly.
			interval := event.lastTimestamp.Sub(event.firstTimestamp.Time) / time.Duration(event.count-1)
			for i := 1; i < int(event.count); i++ {
				latencies = append(latencies, latencyData{
					key:     pod_name,
					latency: event.firstTimestamp.Add(interval * time.Duration(i)).Sub(jobCreateTime),
				})
			}
		}
	}
	sort.Sort(LatencySlice(latencies))
	return NewLatencyMetric(latencies)
}
