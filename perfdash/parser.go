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
	"k8s.io/kubernetes/test/e2e/framework/metrics"
	"k8s.io/kubernetes/test/e2e/perftype"
	"math"
	"os"
	"strconv"
	"time"
)

func stripCount(data *perftype.DataItem) {
	delete(data.Labels, "Count")
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

func parseResponsivenessData(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	obj := perftype.PerfData{}
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
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
	Cpu    float64 `json:"Cpu"`
	Memory int     `json:"Mem"`
}

type resourceUsage struct {
	Cpu    float64
	Memory float64
}
type usageAtPercentiles map[string]resourceUsage
type podNameToUsage map[string]usageAtPercentiles

func parseResourceUsageData(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj resourceUsagePercentiles
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	usage := make(podNameToUsage)
	for percentile, items := range obj {
		for _, item := range items {
			name := RemoveDisambiguationInfixes(item.Name)
			if _, ok := usage[name]; !ok {
				usage[name] = make(usageAtPercentiles)
			}
			cpu, memory := float64(item.Cpu), float64(item.Memory)
			if otherUsage, ok := usage[name][percentile]; ok {
				// Note that we take max of each resource separately, potentially manufacturing a
				// "franken-sample" which was never seen in the wild. We do this hoping that such result
				// will be more stable across runs.
				cpu = math.Max(cpu, otherUsage.Cpu)
				memory = math.Max(memory, otherUsage.Memory)
			}
			usage[name][percentile] = resourceUsage{cpu, memory}
		}
	}
	for podName, usageAtPercentiles := range usage {
		cpu := perftype.DataItem{Unit: "cores", Labels: map[string]string{"PodName": podName, "Resource": "CPU"}, Data: make(map[string]float64)}
		memory := perftype.DataItem{Unit: "MiB", Labels: map[string]string{"PodName": podName, "Resource": "memory"}, Data: make(map[string]float64)}
		for percentile, usage := range usageAtPercentiles {
			cpu.Data[percentile] = usage.Cpu
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
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	if testResult.Version == "" {
		testResult.Version = obj.Version
	}
	if testResult.Version == obj.Version {
		for i := range obj.DataItems {
			if err := createRequestCountData(&obj.DataItems[i]); err != nil {
				fmt.Fprintf(os.Stderr, "error creating request count data in build %d dataItem %d: %v\n", buildNumber, i, err)
				continue
			}
			stripCount(&obj.DataItems[i])
			testResult.Builds[build] = append(testResult.Builds[build], obj.DataItems[i])
		}
	}
}

func parseApiserverRequestCount(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj metrics.MetricsCollection
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	if obj.ApiServerMetrics == nil {
		fmt.Fprintf(os.Stderr, "no ApiServerMetrics data in build %d", buildNumber)
		return
	}
	metric, ok := obj.ApiServerMetrics["apiserver_request_count"]
	if !ok {
		fmt.Fprintf(os.Stderr, "no apiserver_request_count metric data in build %d", buildNumber)
		return
	}
	for i := range metric {
		perfData := perftype.DataItem{Unit: "", Data: make(map[string]float64), Labels: make(map[string]string)}
		for k, v := range metric[i].Metric {
			perfData.Labels[string(k)] = string(v)
		}
		delete(perfData.Labels, "__name__")
		delete(perfData.Labels, "contentType")
		perfData.Data["RequestCount"] = float64(metric[i].Value)
		testResult.Builds[build] = append(testResult.Builds[build], perfData)
	}
}

// TODO(krzysied): Copy of structures from kuberentes repository.
// At some point latencyMetric and schedulingMetrics should be moved to metric package.
type latencyMetric struct {
	Perc50 time.Duration `json:"Perc50"`
	Perc90 time.Duration `json:"Perc90"`
	Perc99 time.Duration `json:"Perc99"`
}

type schedulingMetrics struct {
	PredicateEvaluationLatency  latencyMetric `json:"predicateEvaluationLatency"`
	PriorityEvaluationLatency   latencyMetric `json:"priorityEvaluationLatency"`
	PreemptionEvaluationLatency latencyMetric `json:"preemptionEvaluationLatency"`
	BindingLatency              latencyMetric `json:"bindingLatency"`
	ThroughputAverage           float64       `json:"throughputAverage"`
	ThroughputPerc50            float64       `json:"throughputPerc50"`
	ThroughputPerc90            float64       `json:"throughputPerc90"`
	ThroughputPerc99            float64       `json:"throughputPerc99"`
}

func parseOperationLatency(latency latencyMetric, operationName string) perftype.DataItem {
	perfData := perftype.DataItem{Unit: "ms", Labels: map[string]string{"Operation": operationName}, Data: make(map[string]float64)}
	perfData.Data["Perc50"] = float64(latency.Perc50) / float64(time.Millisecond)
	perfData.Data["Perc90"] = float64(latency.Perc90) / float64(time.Millisecond)
	perfData.Data["Perc99"] = float64(latency.Perc99) / float64(time.Millisecond)
	return perfData
}

func parseSchedulingLatency(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj schedulingMetrics
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	predicateEvaluation := parseOperationLatency(obj.PredicateEvaluationLatency, "predicate_evaluation")
	testResult.Builds[build] = append(testResult.Builds[build], predicateEvaluation)
	priorityEvaluation := parseOperationLatency(obj.PriorityEvaluationLatency, "priority_evaluation")
	testResult.Builds[build] = append(testResult.Builds[build], priorityEvaluation)
	preemptionEvaluation := parseOperationLatency(obj.PreemptionEvaluationLatency, "preemption_evaluation")
	testResult.Builds[build] = append(testResult.Builds[build], preemptionEvaluation)
	binding := parseOperationLatency(obj.BindingLatency, "binding")
	testResult.Builds[build] = append(testResult.Builds[build], binding)
}

func pareseSchedulingThroughput(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj schedulingMetrics
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	perfData := perftype.DataItem{Unit: "1/s", Labels: map[string]string{}, Data: make(map[string]float64)}
	perfData.Data["Perc50"] = obj.ThroughputPerc50
	perfData.Data["Perc90"] = obj.ThroughputPerc90
	perfData.Data["Perc99"] = obj.ThroughputPerc99
	perfData.Data["Average"] = obj.ThroughputAverage
	testResult.Builds[build] = append(testResult.Builds[build], perfData)
}

// TODO(krzysied): This structure also should be moved to metric package.
type histogram struct {
	Labels  map[string]string `json:"labels"`
	Buckets map[string]int    `json:"buckets"`
}

type histogramVec []histogram

// HistogramMetrics is a generalization of histogram metrics structure from metric_util.go
// Instead of explicit fields, we have a map.
type histogramMetrics map[string]histogramVec

func parseHistogramMetric(metricName string) func(data []byte, buildNumber int, testResult *BuildData) {
	return func(data []byte, buildNumber int, testResult *BuildData) {
		testResult.Version = "v1"
		build := fmt.Sprintf("%d", buildNumber)
		var obj histogramMetrics
		if err := json.Unmarshal(data, &obj); err != nil {
			fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
			return
		}
		for k, v := range obj {
			if k == metricName {
				for i := range v {
					perfData := perftype.DataItem{Unit: "%", Labels: v[i].Labels, Data: make(map[string]float64)}
					delete(perfData.Labels, "__name__")
					count, exists := v[i].Buckets["+Inf"]
					if !exists {
						fmt.Fprintf(os.Stderr, "err in build %d: no +Inf bucket: %s\n", buildNumber, string(data))
						continue
					}
					for kBucket, vBucket := range v[i].Buckets {
						if kBucket != "+Inf" {
							perfData.Data["le "+kBucket+"s"] = float64(vBucket) / float64(count) * 100
						}
					}
					testResult.Builds[build] = append(testResult.Builds[build], perfData)
				}
			}
		}
	}
}
