/*
Copyright 2017 The Kubernetes Authors.

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

package scraper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	e2e "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"

	"github.com/golang/glog"
)

const (
	readingJunk              = iota
	readingAPICallLatencies  = iota
	readingPodStartupLatency = iota
	testNameIdentifierTag    = "[BeforeEach] [k8s.io]"
	apiLatencyResultTag      = "API calls latencies: "
	podLatencyResultTag      = "Pod startup latency: "
)

// GetMetricsFromBuildLog takes the contents of a build log file, reads
// and parses JSON summaries of API call and Pod startup latencies.
func GetMetricsFromBuildLog(buildLog []byte) (map[string]*e2e.APIResponsiveness, map[string]*e2e.PodStartupLatency) {
	// Parse using a finite state automaton.
	state := readingJunk
	buff := &bytes.Buffer{}
	testName := ""
	scanner := bufio.NewScanner(bytes.NewReader(buildLog))
	apiCallLatencies := make(map[string]*e2e.APIResponsiveness)
	podStartupLatency := make(map[string]*e2e.PodStartupLatency)
	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case readingJunk:
			if strings.Contains(line, testNameIdentifierTag) {
				testName = strings.Trim(strings.Split(line, testNameIdentifierTag)[1], " ")
				buff.Reset()
			} else if strings.Contains(line, apiLatencyResultTag) {
				state = readingAPICallLatencies
				buff.WriteString("{ ")
			} else if strings.Contains(line, podLatencyResultTag) {
				state = readingPodStartupLatency
				buff.WriteString("{ ")
			}
		case readingAPICallLatencies, readingPodStartupLatency:
			dataStartIndex := strings.Index(line, "]")
			buff.WriteString(line[dataStartIndex+2:] + " ")
			if line[dataStartIndex+2] == '}' { // Reached the end of json
				if state == readingAPICallLatencies {
					apiCallLatencies[testName] = &e2e.APIResponsiveness{}
					if err := json.Unmarshal(buff.Bytes(), apiCallLatencies[testName]); err != nil {
						glog.V(0).Infof("Error parsing APIResponsiveness JSON: %v", err)
					}
				} else {
					podStartupLatency[testName] = &e2e.PodStartupLatency{}
					if err := json.Unmarshal(buff.Bytes(), podStartupLatency[testName]); err != nil {
						glog.V(0).Infof("Error parsing PodStartupLatency JSON: %v", err)
					}
				}
				buff.Reset()
				state = readingJunk
			}
		}
	}
	return apiCallLatencies, podStartupLatency
}

// GetMetricsForRuns is a wrapper for calling GetMetricsForRun on multiple runs returning
// an array of the obtained results. Neglects runs whose metrics could not be fetched.
func GetMetricsForRuns(job string, runs []int, utils util.JobLogUtils) ([]map[string]*e2e.APIResponsiveness, []map[string]*e2e.PodStartupLatency, error) {
	var apiCallLatenciesArray []map[string]*e2e.APIResponsiveness
	var podStartupLatencyArray []map[string]*e2e.PodStartupLatency
	for _, run := range runs {
		buildLog, err := utils.GetJobRunBuildLogContents(job, run)
		if err != nil {
			glog.V(0).Infof("Failed to get build log contents for run %v-%v (skipping it): %v", job, run, err)
			continue
		}
		apiCallLatencies, podStartupLatency := GetMetricsFromBuildLog(buildLog)
		apiCallLatenciesArray = append(apiCallLatenciesArray, apiCallLatencies)
		podStartupLatencyArray = append(podStartupLatencyArray, podStartupLatency)
	}
	return apiCallLatenciesArray, podStartupLatencyArray, nil
}
