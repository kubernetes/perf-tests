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
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"

	"k8s.io/contrib/test-utils/utils"
	"k8s.io/kubernetes/test/e2e/perftype"
)

// GoogleGCSDownloader that gets data about Google results from the GCS repository
type GoogleGCSDownloader struct {
	DefaultBuildsCount   int
	GoogleGCSBucketUtils *utils.Utils
}

// NewGoogleGCSDownloader creates a new GoogleGCSDownloader
func NewGoogleGCSDownloader(defaultBuildsCount int) *GoogleGCSDownloader {
	return &GoogleGCSDownloader{
		DefaultBuildsCount:   defaultBuildsCount,
		GoogleGCSBucketUtils: utils.NewUtils(utils.KubekinsBucket, utils.LogDir),
	}
}

// TODO(random-liu): Only download and update new data each time.
func (g *GoogleGCSDownloader) getData() (JobToCategoryData, error) {
	newJobs, err := getProwConfig()
	if err == nil {
		TestConfig[utils.KubekinsBucket] = newJobs
	} else {
		fmt.Fprintf(os.Stderr, "Failed to refresh config: %v\n", err)
	}
	fmt.Print("Getting Data from GCS...\n")
	result := make(JobToCategoryData)
	var resultLock sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(TestConfig[utils.KubekinsBucket]))
	for job, tests := range TestConfig[utils.KubekinsBucket] {
		if tests.Prefix == "" {
			return result, fmt.Errorf("Invalid empty Prefix for job %s", job)
		}
		for categoryLabel, categoryMap := range tests.Descriptions {
			for testLabel := range categoryMap {
				resultLock.Lock()
				if _, found := result[tests.Prefix]; !found {
					result[tests.Prefix] = make(CategoryToMetricData)
				}
				if _, found := result[tests.Prefix][categoryLabel]; !found {
					result[tests.Prefix][categoryLabel] = make(MetricToBuildData)
				}
				if _, found := result[tests.Prefix][categoryLabel][testLabel]; found {
					return result, fmt.Errorf("Duplicate name %s for %s", testLabel, tests.Prefix)
				}
				result[tests.Prefix][categoryLabel][testLabel] = &BuildData{Job: job, Version: "", Builds: map[string][]perftype.DataItem{}}
				resultLock.Unlock()
			}
		}
		go g.getJobData(&wg, result, &resultLock, job, tests)
	}
	wg.Wait()
	return result, nil
}

func (g *GoogleGCSDownloader) getJobData(wg *sync.WaitGroup, result JobToCategoryData, resultLock *sync.Mutex, job string, tests Tests) {
	defer wg.Done()
	buildNumbers, err := g.GoogleGCSBucketUtils.GetBuildNumbersFromJenkinsGoogleBucket(job)
	if err != nil {
		panic(err)
	}

	buildsToFetch := tests.BuildsCount
	if buildsToFetch < 1 {
		buildsToFetch = g.DefaultBuildsCount
	}
	fmt.Printf("Builds to fetch for %v: %v\n", job, buildsToFetch)

	sort.Sort(sort.Reverse(sort.IntSlice(buildNumbers)))
	for index := 0; index < buildsToFetch && index < len(buildNumbers); index++ {
		buildNumber := buildNumbers[index]
		fmt.Printf("Fetching %s build %v...\n", job, buildNumber)
		for categoryLabel, categoryMap := range tests.Descriptions {
			for testLabel, testDescriptions := range categoryMap {
				for _, testDescription := range testDescriptions {
					fileStem := fmt.Sprintf("artifacts/%v_%v", testDescription.OutputFilePrefix, testDescription.Name)
					artifacts, err := g.GoogleGCSBucketUtils.ListFilesInBuild(job, buildNumber, fileStem)
					if err != nil || len(artifacts) == 0 {
						fmt.Printf("Error while looking for %s* in %s build %v: %v\n", fileStem, job, buildNumber, err)
						continue
					}
					metricsFilename := artifacts[0][strings.LastIndex(artifacts[0], "/")+1:]
					if len(artifacts) > 1 {
						fmt.Printf("WARNING: found multiple %s files with data, reading only one: %s\n", fileStem, metricsFilename)
					}
					testDataResponse, err := g.GoogleGCSBucketUtils.GetFileFromJenkinsGoogleBucket(job, buildNumber, fmt.Sprintf("artifacts/%v", metricsFilename))
					if err != nil {
						panic(err)
					}

					func() {
						testDataBody := testDataResponse.Body
						defer testDataBody.Close()
						data, err := ioutil.ReadAll(testDataBody)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error when reading response Body: %v\n", err)
							return
						}
						resultLock.Lock()
						buildData := result[tests.Prefix][categoryLabel][testLabel]
						resultLock.Unlock()
						testDescription.Parser(data, buildNumber, buildData)
					}()
					break
				}
			}
		}
	}
}
