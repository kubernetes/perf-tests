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
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"k8s.io/kubernetes/test/e2e/perftype"
	"k8s.io/perf-tests/benchmark/pkg/metricsfetcher/util"
)

func TestGetMetricsFilePathsForRun(t *testing.T) {
	utils := util.MockJobLogUtils{
		MockFilesWithPrefix: map[int]map[string][]string{
			220: {
				APICallLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + APICallLatencyFilePrefix + "_testA_xyz123.json",
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + APICallLatencyFilePrefix + "_testB_xy123c.json",
				},
				PodStartupLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + PodStartupLatencyFilePrefix + "_testA_xyz123.json",
				},
				"SomeOtherLatencyFilePrefix": {
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + "SomeOtherLatencyFilePrefix" + "_testA_xyz123.json",
				},
			},
			221: {
				APICallLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/221/" + APICallLatencyFilePrefix + "_testA_xyz123.json",
				},
				PodStartupLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/221/" + PodStartupLatencyFilePrefix + "_testA_xyz123.json",
				},
			},
		},
	}
	metricFiles := GetMetricsFilePathsForRun("foobar", 220, utils)

	expected := map[string][]string{
		"testA": {
			APICallLatencyFilePrefix + "_testA_xyz123.json",
			PodStartupLatencyFilePrefix + "_testA_xyz123.json",
		},
		"testB": {
			APICallLatencyFilePrefix + "_testB_xy123c.json",
		},
	}

	if !reflect.DeepEqual(metricFiles, expected) {
		t.Errorf("Metric files map mismatching from what was expected:\nReal: %v\nExpected: %v", metricFiles, expected)
	}
}

func TestGetMetricsForRun(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	var latencyFilesContents [][]byte
	latencyFiles := []string{"APIResponsiveness_testA_xyz123.txt", "APIResponsiveness_testB_xy123c.txt", "PodStartupLatency_testA_xyz123.txt"}
	for _, latencyFile := range latencyFiles {
		fileContents, err := ioutil.ReadFile(wd + "/test-data/" + latencyFile)
		if err != nil {
			panic(err)
		}
		latencyFilesContents = append(latencyFilesContents, fileContents)
	}

	utils := util.MockJobLogUtils{
		MockFilesWithPrefix: map[int]map[string][]string{
			220: {
				APICallLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + APICallLatencyFilePrefix + "_testA_xyz123.json",
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + APICallLatencyFilePrefix + "_testB_xy123c.json",
				},
				PodStartupLatencyFilePrefix: {
					"gs://kubernetes-jenkins/logs/job/foobar/run/220/" + PodStartupLatencyFilePrefix + "_testA_xyz123.json",
				},
			},
		},
		MockFileContents: map[int]map[string][]byte{
			220: {
				(APICallLatencyFilePrefix + "_testA_xyz123.json"):    latencyFilesContents[0],
				(APICallLatencyFilePrefix + "_testB_xy123c.json"):    latencyFilesContents[1],
				(PodStartupLatencyFilePrefix + "_testA_xyz123.json"): latencyFilesContents[2],
			},
		},
	}
	metrics := GetMetricsForRun("foobar", 220, utils)

	expected := map[string][]perftype.PerfData{
		"testA": {
			{
				Version: "v1",
				DataItems: []perftype.DataItem{
					{
						Data: map[string]float64{
							"Perc50": 4.598,
							"Perc90": 8.63,
							"Perc99": 21.707,
						},
						Unit: "ms",
						Labels: map[string]string{
							"Count":    "6200",
							"Resource": "pods",
							"Verb":     "DELETE",
						},
					},
				},
			},
			{
				Version: "v1",
				DataItems: []perftype.DataItem{
					{
						Data: map[string]float64{
							"Perc50":  1086.056005,
							"Perc90":  1881.996031,
							"Perc99":  2029.913438,
							"Perc100": 2079.704676,
						},
						Unit: "ms",
						Labels: map[string]string{
							"Metric": "pod_startup",
						},
					},
				},
			},
		},
		"testB": {
			{
				Version: "v1",
				DataItems: []perftype.DataItem{
					{
						Data: map[string]float64{
							"Perc50": 16.068,
							"Perc90": 20.138,
							"Perc99": 45.424,
						},
						Unit: "ms",
						Labels: map[string]string{
							"Count":    "328",
							"Resource": "services",
							"Verb":     "DELETE",
						},
					},
					{
						Data: map[string]float64{
							"Perc50": 2.633,
							"Perc90": 4.682,
							"Perc99": 14.187,
						},
						Unit: "ms",
						Labels: map[string]string{
							"Count":    "9765",
							"Resource": "nodes",
							"Verb":     "PATCH",
						},
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(metrics, expected) {
		t.Errorf("Metric map mismatching from what was expected:\nReal: %v\nExpected: %v", metrics, expected)
	}
}
