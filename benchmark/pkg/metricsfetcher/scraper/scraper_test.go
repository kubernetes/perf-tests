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

	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func TestGetMetricsFromBuildLog(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	buildLog, err := ioutil.ReadFile(wd + "/test-data/build-log.txt")
	if err != nil {
		panic(err)
	}
	apiCallLatencies, podStartupLatency := GetMetricsFromBuildLog(buildLog)

	// Check API call latencies for load test.
	if latencies, present := apiCallLatencies["Load capacity"]; !present {
		t.Errorf("Couldn't extract API call latencies for load test")
	} else {
		if len(latencies.APICalls) != 10 {
			t.Errorf("API call latencies count mismatched for load test: <expected:10,real:%v>", len(latencies.APICalls))
		}
		expected := e2e.APICall{
			Resource: "pods",
			Verb:     "DELETE",
			Latency: e2e.LatencyMetric{
				Perc50:  6556000,
				Perc90:  15860000,
				Perc99:  50408000,
				Perc100: 0,
			},
		}
		if !reflect.DeepEqual(latencies.APICalls[0], expected) {
			t.Errorf("Latencies of sample API call (delete pods) mismatching for load test")
		}
	}

	// Check API call latencies for density test.
	if latencies, present := apiCallLatencies["Density"]; !present {
		t.Errorf("Couldn't extract API call latencies for density test")
	} else {
		if len(latencies.APICalls) != 3 {
			t.Errorf("API call latencies count mismatched for load test: <expected:3,real:%v>", len(latencies.APICalls))
		}
		expected := e2e.APICall{
			Resource: "pods",
			Verb:     "DELETE",
			Latency: e2e.LatencyMetric{
				Perc50:  5509000,
				Perc90:  12512000,
				Perc99:  41400000,
				Perc100: 0,
			},
		}
		if !reflect.DeepEqual(latencies.APICalls[0], expected) {
			t.Errorf("Latencies of sample API call (delete pods) mismatching for density test")
		}
	}

	// Check pod startup latency for density test.
	if latency, present := podStartupLatency["Density"]; !present {
		t.Errorf("Couldn't extract pod startup latency for density test")
	} else {
		expected := e2e.PodStartupLatency{
			Latency: e2e.LatencyMetric{
				Perc50:  1194643832,
				Perc90:  1746599261,
				Perc99:  2064082893,
				Perc100: 4215510634,
			},
		}
		if !reflect.DeepEqual(*latency, expected) {
			t.Errorf("Latency of pod startup mismatching for density test")
		}
	}
}
