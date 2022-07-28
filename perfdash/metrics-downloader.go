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
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog"
	"k8s.io/kubernetes/test/e2e/perftype"
)

// DownloaderOptions is an options for Downloader.
type DownloaderOptions struct {
	Mode               string
	ConfigPaths        []string
	GithubConfigDirs   []string
	DefaultBuildsCount int
	// Development-only flag.
	// Overrides build count from "perfDashBuildsCount" label with DefaultBuildsCount.
	OverrideBuildCount bool
}

// Downloader that gets data about results from a storage service (GCS) repository.
type Downloader struct {
	MetricsBkt              MetricsBucket
	Options                 *DownloaderOptions
	allowParsersForAllTests bool
}

// NewDownloader creates a new Downloader.
func NewDownloader(opt *DownloaderOptions, bkt MetricsBucket, allowAllParsers bool) *Downloader {
	return &Downloader{
		MetricsBkt:              bkt,
		Options:                 opt,
		allowParsersForAllTests: allowAllParsers,
	}
}

// TODO(random-liu): Only download and update new data each time.
func (g *Downloader) getData() (JobToCategoryData, error) {
	configPaths := make([]string, len(g.Options.ConfigPaths))
	copy(configPaths, g.Options.ConfigPaths)
	for _, githubURL := range g.Options.GithubConfigDirs {
		githubConfigPaths, err := GetConfigsFromGithub(githubURL)
		if err != nil {
			return nil, err
		}
		configPaths = append(configPaths, githubConfigPaths...)
	}

	klog.Infof("Config paths - %d", len(configPaths))
	for i, configPath := range configPaths {
		klog.Infof("Config path %d: %s", i+1, configPath)
	}

	newJobs, err := getProwConfig(configPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh config: %v", err)
	}
	klog.Infof("Getting Data from %v...", options.Mode)
	result := make(JobToCategoryData)
	var resultLock sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(newJobs))
	for job, tests := range newJobs {
		if tests.Prefix == "" {
			return nil, fmt.Errorf("invalid empty Prefix for job %s", job)
		}
		go g.getJobData(&wg, result, &resultLock, job, tests)
	}
	wg.Wait()
	return result, nil
}

type artifactsCache struct {
	cache  map[string][]string
	bucket MetricsBucket
}

func newArtifactsCache(bucket MetricsBucket) artifactsCache {
	return artifactsCache{
		cache:  make(map[string][]string),
		bucket: bucket,
	}
}

func (a *artifactsCache) getMatchingFiles(job string, buildNumber int, prefix string) ([]string, error) {
	dirPrefix := a.bucket.GetFilePrefix(job, buildNumber, "/")
	searchPrefix := a.bucket.GetFilePrefix(job, buildNumber, prefix)

	if _, ok := a.cache[dirPrefix]; !ok {
		result, err := a.bucket.ListFilesInBuild(job, buildNumber, "/")
		if err != nil {
			return nil, err
		}

		a.cache[dirPrefix] = result
	}

	val, ok := a.cache[dirPrefix]
	if !ok {
		return nil, fmt.Errorf("couldn't get list of files from cache")
	}
	matchedFiles := []string{}
	for _, file := range val {
		if strings.HasPrefix(file, searchPrefix) {
			matchedFiles = append(matchedFiles, file)
		}
	}

	return matchedFiles, nil
}

/*
getJobData fetches build numbers, reads metrics data from GCS and
updates result with parsed metrics for a given prow job. Assumptions:
- metric files are in /artifacts directory
- metric file names have following prefix: {{OutputFilePrefix}}_{{Name}},
  where OutputFilePrefix and Name are parts of test description (specified in prefdash config)
- if there are multiple files with a given prefix, then expected format is
  {{OutputFilePrefix}}_{{Name}}_{{SuiteId}}. SuiteId is prepended to the category label,
  which allows comparing metrics across several runs in a given suite
*/
func (g *Downloader) getJobData(wg *sync.WaitGroup, result JobToCategoryData, resultLock *sync.Mutex, job string, tests Tests) {
	defer wg.Done()
	buildNumbers, err := g.MetricsBkt.GetBuildNumbers(job)
	if err != nil {
		panic(err)
	}

	buildsToFetch := tests.BuildsCount
	if buildsToFetch < 1 || g.Options.OverrideBuildCount {
		buildsToFetch = g.Options.DefaultBuildsCount
	}
	klog.Infof("Builds to fetch for %v: %v", job, buildsToFetch)

	sort.Sort(sort.Reverse(sort.IntSlice(buildNumbers)))
	for index := 0; index < buildsToFetch && index < len(buildNumbers); index++ {
		buildNumber := buildNumbers[index]
		cache := newArtifactsCache(g.MetricsBkt)
		klog.Infof("Fetching %s build %v...", job, buildNumber)
		for categoryLabel, categoryMap := range tests.Descriptions {
			for testLabel, testDescriptions := range categoryMap {
				for _, testDescription := range testDescriptions {
					if !g.allowParsersForAllTests && testDescription.Name == "" {
						continue
					}
					filePrefix := testDescription.OutputFilePrefix
					if testDescription.Name != "" {
						filePrefix = fmt.Sprintf("%v_%v", filePrefix, testDescription.Name)
					}
					searchPrefix := g.artifactName(tests, filePrefix)

					artifacts, err := cache.getMatchingFiles(job, buildNumber, searchPrefix)
					if err != nil || len(artifacts) == 0 {
						klog.Infof("Error while looking for %s* in %s build %v: %v", searchPrefix, job, buildNumber, err)
						continue
					}

					for _, artifact := range artifacts {
						metricsFileName := filepath.Base(artifact)
						resultCategory := getResultCategory(metricsFileName, filePrefix, categoryLabel, artifacts, testDescription.FetchMetricNameFromArtifact)
						fileName := g.artifactName(tests, metricsFileName)
						testDataResponse, err := g.MetricsBkt.ReadFile(job, buildNumber, fileName)
						if err != nil {
							klog.Infof("Error when reading response Body for %q: %v", fileName, err)
							continue
						}
						if testDescription.FetchMetricNameFromArtifact {
							trimmed := strings.TrimPrefix(metricsFileName, filePrefix+" ")
							testLabel = strings.Split(trimmed, "_")[0]
						}
						buildData := getBuildData(result, tests.Prefix, resultCategory, testLabel, job, resultLock)
						testDescription.Parser(testDataResponse, buildNumber, buildData)
					}
				}
			}
		}
	}
}

func (g *Downloader) artifactName(jobAttrs Tests, file string) string {
	return path.Join(jobAttrs.ArtifactsDir, file)
}

func getResultCategory(metricsFileName string, filePrefix string, category string, artifacts []string, forceConstantCategory bool) string {
	if len(artifacts) <= 1 || forceConstantCategory {
		return category
	}
	// If there are more artifacts, assume that this is a test suite run.
	trimmed := strings.TrimPrefix(metricsFileName, filePrefix+"_")
	suiteID := strings.Split(trimmed, "_")[0]
	return fmt.Sprintf("%v_%v", suiteID, category)
}

func getBuildData(result JobToCategoryData, prefix string, category string, label string, job string, resultLock *sync.Mutex) *BuildData {
	resultLock.Lock()
	defer resultLock.Unlock()
	if _, found := result[prefix]; !found {
		result[prefix] = make(CategoryToMetricData)
	}
	if _, found := result[prefix][category]; !found {
		result[prefix][category] = make(MetricToBuildData)
	}
	if _, found := result[prefix][category][label]; !found {
		result[prefix][category][label] = &BuildData{Job: job, Version: "", Builds: map[string][]perftype.DataItem{}}
	}
	return result[prefix][category][label]
}

// MetricsBucket is the interface that fetches data from a storage service.
type MetricsBucket interface {
	GetBuildNumbers(job string) ([]int, error)
	ListFilesInBuild(job string, buildNumber int, prefix string) ([]string, error)
	GetFilePrefix(job string, buildNumber int, prefix string) string
	ReadFile(job string, buildNumber int, path string) ([]byte, error)
}

func joinStringsAndInts(pathElements ...interface{}) string {
	var parts []string
	for _, e := range pathElements {
		switch t := e.(type) {
		case string:
			parts = append(parts, t)
		case int:
			parts = append(parts, strconv.Itoa(t))
		default:
			panic(fmt.Sprintf("joinStringsAndInts only accepts ints and strings as path elements, but was passed %#v", t))
		}
	}
	return path.Join(parts...)
}
