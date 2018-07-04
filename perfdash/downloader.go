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
	"net/http"
	"sort"

	"k8s.io/kubernetes/test/e2e/perftype"
)

// Downloader is the interface that gets a data from a predefined source.
type Downloader interface {
	getData() (TestToBuildData, error)
}

// BuildData contains job name and a map from build number to perf data.
type BuildData struct {
	Builds  map[string][]perftype.DataItem `json:"builds"`
	Job     string                         `json:"job"`
	Version string                         `json:"version"`
}

// TestToBuildData is a map from test name to BuildData pointer.
// TODO(random-liu): Use a more complex data structure if we need to support more test in the future.
type TestToBuildData map[string]*BuildData

// JobToTestData is a map from job name to TestToBuildData.
type JobToTestData map[string]TestToBuildData

func serveHTTPObject(res http.ResponseWriter, req *http.Request, obj interface{}) {
	data, err := json.Marshal(obj)
	if err != nil {
		res.Header().Set("Content-type", "text/html")
		res.WriteHeader(http.StatusInternalServerError)
		res.Write([]byte(fmt.Sprintf("<h3>Internal Error</h3><p>%v", err)))
		return
	}
	res.Header().Set("Content-type", "application/json")
	res.WriteHeader(http.StatusOK)
	res.Write(data)
}

func getURLParam(req *http.Request, name string) (string, bool) {
	params, ok := req.URL.Query()[name]
	if !ok || len(params) < 1 {
		return "", false
	}
	return params[0], true
}

// ServeJobNames serves all available job names.
func (j *JobToTestData) ServeJobNames(res http.ResponseWriter, req *http.Request) {
	jobNames := make([]string, 0)
	if j != nil {
		for k := range *j {
			jobNames = append(jobNames, k)
		}
	}
	sort.Strings(jobNames)
	serveHTTPObject(res, req, &jobNames)
}

// ServeTestNames serves all available test name for given job.
func (j *JobToTestData) ServeTestNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok {
		fmt.Printf("Url Param 'jobname' is missing\n")
		return
	}

	tests, ok := (*j)[jobname]
	if !ok {
		fmt.Printf("unknown jobname - %v\n", jobname)
		return
	}

	testNames := make([]string, 0)
	for k := range tests {
		testNames = append(testNames, k)
	}
	sort.Strings(testNames)
	serveHTTPObject(res, req, &testNames)
}

// ServeBuildsData serves builds data for given job name and test name.
func (j *JobToTestData) ServeBuildsData(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok {
		fmt.Printf("Url Param 'jobname' is missing\n")
		return
	}
	testname, ok := getURLParam(req, "testname")
	if !ok {
		fmt.Printf("Url Param 'testname' is missing\n")
		return
	}

	tests, ok := (*j)[jobname]
	if !ok {
		fmt.Printf("unknown jobname - %v\n", jobname)
		return
	}
	builds, ok := tests[testname]
	if !ok {
		fmt.Printf("unknown testname - %v\n", testname)
		return
	}

	serveHTTPObject(res, req, builds)
}
