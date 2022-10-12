/*
Copyright 2016 The Kubernetes Authors.

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

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog"

	"k8s.io/kubernetes/test/e2e/framework/metrics"
	"k8s.io/kubernetes/test/e2e/perftype"
)

func stripCount(data *perftype.DataItem) {
	delete(data.Labels, "Count")
	delete(data.Labels, "SlowCount")
}

func createRequestCountData(data *perftype.DataItem) error {
	data.Unit = ""
	data.Data = make(map[string]float64)
	requestCountString, ok := data.Labels["Count"]
	if !ok {
		return fmt.Errorf("no 'Count' label")
	}
	requestCount, err := strconv.ParseFloat(requestCountString, 64)
	if err != nil {
		return fmt.Errorf("couldn't parse count: %v", err)
	}
	data.Data["RequestCount"] = requestCount
	return nil
}

type ContainerRestartsInfo struct {
	Container    string
	Pod          string
	Namespace    string
	RestartCount int
}

func parseContainerRestarts(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	restarts := make([]ContainerRestartsInfo, 0)
	if err := json.Unmarshal(data, &restarts); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}

	restartsByContainer := make(map[string]float64)
	for _, restart := range restarts {
		restartsByContainer[restart.Container] = restartsByContainer[restart.Container] + float64(restart.RestartCount)
	}

	res := perftype.DataItem{Unit: "", Labels: map[string]string{"RestartCount": "RestartCount"}, Data: restartsByContainer}
	testResult.Builds[build] = append(testResult.Builds[build], res)
}

func parsePerfData(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	obj := perftype.PerfData{}
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}
	if testResult.Version == "" {
		testResult.Version = obj.Version
	}
	if testResult.Version == obj.Version {
		for i := range obj.DataItems {
			stripCount(&obj.DataItems[i])
			testResult.Builds[build] = append(testResult.Builds[build], obj.DataItems[i])
		}
	}
}

type resourceUsagePercentiles map[string][]resourceUsages

type resourceUsages struct {
	Name   string  `json:"Name"`
	CPU    float64 `json:"CPU"`
	Memory int     `json:"Mem"`
}

type resourceUsage struct {
	CPU    float64
	Memory float64
}
type usageAtPercentiles map[string]resourceUsage
type podNameToUsage map[string]usageAtPercentiles

func parseResourceUsageData(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj resourceUsagePercentiles
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}
	usage := make(podNameToUsage)
	for percentile, items := range obj {
		for _, item := range items {
			name := RemoveDisambiguationInfixes(item.Name)
			if _, ok := usage[name]; !ok {
				usage[name] = make(usageAtPercentiles)
			}
			cpu, memory := float64(item.CPU), float64(item.Memory)
			if otherUsage, ok := usage[name][percentile]; ok {
				// Note that we take max of each resource separately, potentially manufacturing a
				// "franken-sample" which was never seen in the wild. We do this hoping that such result
				// will be more stable across runs.
				cpu = math.Max(cpu, otherUsage.CPU)
				memory = math.Max(memory, otherUsage.Memory)
			}
			usage[name][percentile] = resourceUsage{cpu, memory}
		}
	}
	for podName, usageAtPercentiles := range usage {
		cpu := perftype.DataItem{Unit: "cores", Labels: map[string]string{"PodName": podName, "Resource": "CPU"}, Data: make(map[string]float64)}
		memory := perftype.DataItem{Unit: "MiB", Labels: map[string]string{"PodName": podName, "Resource": "memory"}, Data: make(map[string]float64)}
		for percentile, usage := range usageAtPercentiles {
			cpu.Data[percentile] = usage.CPU
			memory.Data[percentile] = usage.Memory / (1024 * 1024)
		}
		testResult.Builds[build] = append(testResult.Builds[build], cpu)
		testResult.Builds[build] = append(testResult.Builds[build], memory)
	}
}

func parseRequestCountData(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	obj := perftype.PerfData{}
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}
	if testResult.Version == "" {
		testResult.Version = obj.Version
	}
	if testResult.Version == obj.Version {
		for i := range obj.DataItems {
			if err := createRequestCountData(&obj.DataItems[i]); err != nil {
				klog.Errorf("error creating request count data in build %d dataItem %d: %v", buildNumber, i, err)
				continue
			}
			stripCount(&obj.DataItems[i])
			testResult.Builds[build] = append(testResult.Builds[build], obj.DataItems[i])
		}
	}
}

var commitMatcher = regexp.MustCompile("kubernetes/.{7}")
var versionMatcher = regexp.MustCompile(`\/v?\d+\.\d+.\d+`)

func parseApiserverRequestCount(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj metrics.Collection
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}
	if obj.APIServerMetrics == nil {
		klog.Errorf("no ApiServerMetrics data in build %d", buildNumber)
		return
	}
	metric, ok := obj.APIServerMetrics["apiserver_request_count"]
	if !ok {
		klog.Errorf("no apiserver_request_count metric data in build %d", buildNumber)
		return
	}
	resultMap := make(map[string]*perftype.DataItem)
	for i := range metric {
		perfData := perftype.DataItem{Unit: "", Data: make(map[string]float64), Labels: make(map[string]string)}
		for k, v := range metric[i].Metric {
			perfData.Labels[string(k)] = string(v)
		}
		delete(perfData.Labels, "__name__")
		delete(perfData.Labels, "contentType")
		dataLabel := "RequestCount"
		if client, ok := perfData.Labels["client"]; ok {
			// Client label contains kubernetes version, which is different
			// in every build. This causes unnecessary creation on multiple different label sets
			// for one metric.
			// This fix removes kubernetes version from client label.
			newClient := commitMatcher.ReplaceAllString(client, "kubernetes")
			if version := versionMatcher.Find([]byte(newClient)); version != nil {
				dataLabel = string(version)
				newClient = strings.Replace(newClient, dataLabel, "", 1)
			}
			perfData.Labels["client"] = newClient
		}
		perfData.Data[dataLabel] = float64(metric[i].Value)
		key := createMapID(perfData.Labels)
		if result, exists := resultMap[key]; exists {
			result.Data[dataLabel] += perfData.Data[dataLabel]
			continue
		}
		resultMap[key] = &perfData
		testResult.Builds[build] = append(testResult.Builds[build], perfData)
	}
}

func parseApiserverInitEventsCount(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj metrics.Collection
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}
	if obj.APIServerMetrics == nil {
		klog.Errorf("no ApiServerMetrics data in build %d", buildNumber)
		return
	}
	metric, ok := obj.APIServerMetrics["apiserver_init_events_total"]
	if !ok {
		klog.Errorf("no apiserver_init_events_total metric data in build %d", buildNumber)
		return
	}
	for i := range metric {
		perfData := perftype.DataItem{Unit: "", Data: make(map[string]float64), Labels: make(map[string]string)}
		for k, v := range metric[i].Metric {
			perfData.Labels[string(k)] = string(v)
		}
		delete(perfData.Labels, "__name__")
		perfData.Data["InitEventsCount"] = float64(metric[i].Value)
		testResult.Builds[build] = append(testResult.Builds[build], perfData)
	}
}

func createMapID(m map[string]string) string {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(fmt.Sprintf("%s:%s|", key, m[key]))
	}
	return b.String()
}

// TODO(krzysied): Copy of structures from kuberentes repository.
// At some point latencyMetric and schedulingMetrics should be moved to metric package.
type latencyMetric struct {
	Perc50 time.Duration `json:"Perc50"`
	Perc90 time.Duration `json:"Perc90"`
	Perc99 time.Duration `json:"Perc99"`
}

type schedulingMetrics struct {
	PreemptionEvaluationLatency latencyMetric `json:"preemptionEvaluationLatency"`
	E2eSchedulingLatency        latencyMetric `json:"e2eSchedulingLatency"`
	SchedulingLatency           latencyMetric `json:"schedulingLatency"`

	FrameworkExtensionPointDuration map[string]latencyMetric `json:"frameworkExtensionPointDuration"`
}

func parseOperationLatency(latency latencyMetric, testName string, operationName string) perftype.DataItem {
	perfData := perftype.DataItem{Unit: "ms", Labels: map[string]string{"TestName": testName, "Operation": operationName}, Data: make(map[string]float64)}
	perfData.Data["Perc50"] = float64(latency.Perc50) / float64(time.Millisecond)
	perfData.Data["Perc90"] = float64(latency.Perc90) / float64(time.Millisecond)
	perfData.Data["Perc99"] = float64(latency.Perc99) / float64(time.Millisecond)
	return perfData
}

func parseSchedulingLatency(testName string) func([]byte, int, *BuildData) {
	return func(data []byte, buildNumber int, testResult *BuildData) {
		testResult.Version = "v1"
		build := fmt.Sprintf("%d", buildNumber)
		var obj schedulingMetrics
		if err := json.Unmarshal(data, &obj); err != nil {
			klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
			return
		}
		preemptionEvaluation := parseOperationLatency(obj.PreemptionEvaluationLatency, testName, "preemption_evaluation")
		testResult.Builds[build] = append(testResult.Builds[build], preemptionEvaluation)
		e2eScheduling := parseOperationLatency(obj.E2eSchedulingLatency, testName, "e2eScheduling")
		testResult.Builds[build] = append(testResult.Builds[build], e2eScheduling)
		scheduling := parseOperationLatency(obj.SchedulingLatency, testName, "scheduling")
		testResult.Builds[build] = append(testResult.Builds[build], scheduling)

		for name, metric := range obj.FrameworkExtensionPointDuration {
			frameworkExtensionPointDuration := parseOperationLatency(metric, testName, name)
			testResult.Builds[build] = append(testResult.Builds[build], frameworkExtensionPointDuration)
		}
	}
}

type schedulingThroughputMetric struct {
	Average float64 `json:"average"`
	Max     float64 `json:"max"`
	Perc50  float64 `json:"perc50"`
	Perc90  float64 `json:"perc90"`
	Perc99  float64 `json:"perc99"`
}

func parseSchedulingThroughputCL(testName string) func([]byte, int, *BuildData) {
	return func(data []byte, buildNumber int, testResult *BuildData) {
		testResult.Version = "v1"
		build := fmt.Sprintf("%d", buildNumber)
		var obj schedulingThroughputMetric
		if err := json.Unmarshal(data, &obj); err != nil {
			klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
			return
		}
		perfData := perftype.DataItem{Unit: "1/s", Labels: map[string]string{"TestName": testName}, Data: make(map[string]float64)}
		perfData.Data["Perc50"] = obj.Perc50
		perfData.Data["Perc90"] = obj.Perc90
		perfData.Data["Perc99"] = obj.Perc99
		perfData.Data["Average"] = obj.Average
		perfData.Data["Max"] = obj.Max
		testResult.Builds[build] = append(testResult.Builds[build], perfData)
	}
}

// TODO(krzysied): This structure also should be moved to metric package.
type histogram struct {
	Labels  map[string]string `json:"labels"`
	Buckets map[string]int    `json:"buckets"`
}

type histogramVec []histogram

type etcdMetrics struct {
	BackendCommitDuration     histogramVec `json:"backendCommitDuration"`
	SnapshotSaveTotalDuration histogramVec `json:"snapshotSaveTotalDuration"`
	PeerRoundTripTime         histogramVec `json:"peerRoundTripTime"`
	WalFsyncDuration          histogramVec `json:"walFsyncDuration"`
	MaxDatabaseSize           float64      `json:"maxDatabaseSize"`
}

func parseHistogramMetric(metricName string) func(data []byte, buildNumber int, testResult *BuildData) {
	return func(data []byte, buildNumber int, testResult *BuildData) {
		testResult.Version = "v1"
		build := fmt.Sprintf("%d", buildNumber)
		var obj etcdMetrics
		if err := json.Unmarshal(data, &obj); err != nil {
			klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
			return
		}

		var histogramVecMetric histogramVec
		switch metricName {
		case "backendCommitDuration":
			histogramVecMetric = obj.BackendCommitDuration
		case "snapshotSaveTotalDuration":
			histogramVecMetric = obj.SnapshotSaveTotalDuration
		case "peerRoundTripTime":
			histogramVecMetric = obj.PeerRoundTripTime
		case "walFsyncDuration":
			histogramVecMetric = obj.WalFsyncDuration
		default:
			klog.Errorf("unknown metric name: %s", metricName)
		}

		for i := range histogramVecMetric {
			perfData := perftype.DataItem{Unit: "%", Labels: histogramVecMetric[i].Labels, Data: make(map[string]float64)}
			delete(perfData.Labels, "__name__")
			count, exists := histogramVecMetric[i].Buckets["+Inf"]
			if !exists {
				klog.Errorf("err in build %d: no +Inf bucket: %s", buildNumber, string(data))
				continue
			}
			for bucket, buckerVal := range histogramVecMetric[i].Buckets {
				if bucket != "+Inf" {
					if count == 0 {
						perfData.Data["<= "+bucket+"s"] = 0
						continue
					}
					perfData.Data["<= "+bucket+"s"] = float64(buckerVal) / float64(count) * 100
				}
			}
			testResult.Builds[build] = append(testResult.Builds[build], perfData)
		}
	}
}

func parseSystemPodMetrics(data []byte, buildNumber int, testResult *BuildData) {
	type containerMetrics struct {
		Name         string `json:"name"`
		RestartCount int32  `json:"restartCount"`
	}

	type podMetrics struct {
		Name       string             `json:"name"`
		Containers []containerMetrics `json:"containers"`
	}

	type systemPodsMetrics struct {
		Pods []podMetrics `json:"pods"`
	}

	build := fmt.Sprintf("%d", buildNumber)
	var obj systemPodsMetrics
	if err := json.Unmarshal(data, &obj); err != nil {
		klog.Errorf("error parsing JSON in build %d: %v %s", buildNumber, err, string(data))
		return
	}

	restartCounts := make(map[string]float64)
	for _, pod := range obj.Pods {
		for _, container := range pod.Containers {
			cnt := float64(container.RestartCount)
			if v, ok := restartCounts[container.Name]; ok {
				restartCounts[container.Name] = math.Max(v, cnt)
			} else {
				restartCounts[container.Name] = cnt
			}
		}
	}

	perfData := perftype.DataItem{
		Unit: "",
		Labels: map[string]string{
			"RestartCount": "RestartCount",
		},
		Data: restartCounts,
	}
	testResult.Builds[build] = append(testResult.Builds[build], perfData)
}
