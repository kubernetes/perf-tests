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
	"sync"

	"k8s.io/klog"
	"k8s.io/kubernetes/test/e2e/perftype"
)

// BuildData contains job name and a map from build number to perf data.
type BuildData struct {
	Builds  Builds `json:"builds"`
	Job     string `json:"job"`
	Version string `json:"version"`
}

// Builds is a structure contains build number to perf data map guarded against concurent modification
type Builds struct {
	builds map[string][]perftype.DataItem
	mu     *sync.RWMutex
}

func (b *Builds) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.builds)
}

func (b *Builds) UnmarshalJSON(data []byte) error {
	b.mu = &sync.RWMutex{}
	return json.Unmarshal(data, &b.builds)
}

// NewBuilds returns a guarded build number to perf data map.
func NewBuilds(builds map[string][]perftype.DataItem) Builds {
	if builds == nil {
		builds = make(map[string][]perftype.DataItem)
	}
	return Builds{
		builds: builds,
		mu:     &sync.RWMutex{},
	}
}

func (b *Builds) AddBuildData(buildNumber string, data perftype.DataItem) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.builds[buildNumber] = append(b.builds[buildNumber], data)
}

func (b *Builds) Builds(buildNumber string) []perftype.DataItem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.builds[buildNumber]
}

// MetricToBuildData is a map from metric name to BuildData pointer.
// TODO(random-liu): Use a more complex data structure if we need to support more test in the future.
type MetricToBuildData map[string]*BuildData

// CategoryToMetricData is a map from category name to MetricToBuildData.
type CategoryToMetricData map[string]MetricToBuildData

// JobToCategoryData is a map from job name to CategoryToMetricData.
type JobToCategoryData map[string]CategoryToMetricData

func serveHTTPObject(res http.ResponseWriter, _ *http.Request, obj interface{}) {
	data, err := json.Marshal(obj)
	if err != nil {
		res.Header().Set("Content-type", "text/html")
		res.WriteHeader(http.StatusInternalServerError)
		_, err = res.Write([]byte(fmt.Sprintf("<h3>Internal Error</h3><p>%v", err)))
		if err != nil {
			klog.Errorf("unable to write error %v", err)
		}
		return
	}
	res.Header().Set("Content-type", "application/json")
	res.WriteHeader(http.StatusOK)
	_, err = res.Write(data)
	if err != nil {
		klog.Errorf("unable to write response data %v", err)
	}
}

func getURLParam(req *http.Request, name string) (string, bool) {
	params, ok := req.URL.Query()[name]
	if !ok || len(params) < 1 {
		return "", false
	}
	return params[0], true
}

// ServeJobNames serves all available job names.
func (j *JobToCategoryData) ServeJobNames(res http.ResponseWriter, req *http.Request) {
	jobNames := make([]string, 0)
	if j != nil {
		for k := range *j {
			jobNames = append(jobNames, k)
		}
	}
	sort.Strings(jobNames)
	serveHTTPObject(res, req, &jobNames)
}

// ServeCategoryNames serves all available category names for given job.
func (j *JobToCategoryData) ServeCategoryNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok {
		klog.Warningf("url Param 'jobname' is missing")
		return
	}

	tests, ok := (*j)[jobname]
	if !ok {
		klog.Infof("unknown jobname - %v", jobname)
		return
	}

	categorynames := make([]string, 0)
	for k := range tests {
		categorynames = append(categorynames, k)
	}
	sort.Strings(categorynames)
	serveHTTPObject(res, req, &categorynames)
}

// ServeMetricNames serves all available metric names for given job and category.
func (j *JobToCategoryData) ServeMetricNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok {
		klog.Warningf("Url Param 'jobname' is missing")
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok {
		klog.Warningf("Url Param 'metriccategoryname' is missing")
		return
	}

	categories, ok := (*j)[jobname]
	if !ok {
		klog.Infof("unknown jobname - %v", jobname)
		return
	}
	tests, ok := categories[categoryname]
	if !ok {
		klog.Infof("unknown metriccategoryname - %v", categoryname)
		return
	}

	metricnames := make([]string, 0)
	for k := range tests {
		metricnames = append(metricnames, k)
	}
	sort.Strings(metricnames)
	serveHTTPObject(res, req, &metricnames)
}

// ServeBuildsData serves builds data for given job name, category name and test name.
func (j *JobToCategoryData) ServeBuildsData(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok {
		klog.Warningf("Url Param 'jobname' is missing")
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok {
		klog.Warningf("Url Param 'metriccategoryname' is missing")
		return
	}
	metricname, ok := getURLParam(req, "metricname")
	if !ok {
		klog.Warningf("Url Param 'metricname' is missing")
		return
	}

	categories, ok := (*j)[jobname]
	if !ok {
		klog.Infof("unknown jobname - %v", jobname)
		return
	}
	tests, ok := categories[categoryname]
	if !ok {
		klog.Infof("unknown metriccategoryname - %v", categoryname)
		return
	}
	builds, ok := tests[metricname]
	if !ok {
		klog.Infof("unknown metricname - %v", metricname)
		return
	}

	serveHTTPObject(res, req, builds)
}
