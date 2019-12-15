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
	"context"
	"fmt"
	"io/ioutil"
	"k8s.io/klog"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"k8s.io/kubernetes/test/e2e/perftype"
)

// GoogleGCSDownloaderOptions is an options for GoogleGCSDownloader.
type GoogleGCSDownloaderOptions struct {
	ConfigPaths        []string
	GithubConfigDirs   []string
	DefaultBuildsCount int
	LogsBucket         string
	LogsPath           string
	CredentialPath     string
	// Development-only flag.
	// Overrides build count from "perfDashBuildsCount" label with DefaultBuildsCount.
	OverrideBuildCount bool
}

// GoogleGCSDownloader that gets data about Google results from the GCS repository.
type GoogleGCSDownloader struct {
	GoogleGCSBucketUtils *bucketUtil
	Options              *GoogleGCSDownloaderOptions
}

// NewGoogleGCSDownloader creates a new GoogleGCSDownloader.
func NewGoogleGCSDownloader(opt *GoogleGCSDownloaderOptions) (*GoogleGCSDownloader, error) {
	b, err := newBucketUtil(opt.LogsBucket, opt.LogsPath, opt.CredentialPath)
	if err != nil {
		return nil, err
	}
	return &GoogleGCSDownloader{
		GoogleGCSBucketUtils: b,
		Options:              opt,
	}, nil
}

// TODO(random-liu): Only download and update new data each time.
func (g *GoogleGCSDownloader) getData() (JobToCategoryData, error) {
	configPaths := make([]string, len(g.Options.ConfigPaths))
	copy(configPaths, g.Options.ConfigPaths)
	for _, githubUrl := range g.Options.GithubConfigDirs {
		githubConfigPaths, err := GetConfigsFromGithub(githubUrl)
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
	klog.Infof("Getting Data from GCS...")
	result := make(JobToCategoryData)
	var resultLock sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(newJobs))
	for job, tests := range newJobs {
		if tests.Prefix == "" {
			return nil, fmt.Errorf("Invalid empty Prefix for job %s", job)
		}
		go g.getJobData(&wg, result, &resultLock, job, tests)
	}
	wg.Wait()
	return result, nil
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
func (g *GoogleGCSDownloader) getJobData(wg *sync.WaitGroup, result JobToCategoryData, resultLock *sync.Mutex, job string, tests Tests) {
	defer wg.Done()
	buildNumbers, err := g.GoogleGCSBucketUtils.getBuildNumbersFromJenkinsGoogleBucket(job)
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
		klog.Infof("Fetching %s build %v...", job, buildNumber)
		for categoryLabel, categoryMap := range tests.Descriptions {
			for testLabel, testDescriptions := range categoryMap {
				for _, testDescription := range testDescriptions {
					filePrefix := fmt.Sprintf("%v_%v", testDescription.OutputFilePrefix, testDescription.Name)
					searchPrefix := fmt.Sprintf("artifacts/%v", filePrefix)
					artifacts, err := g.GoogleGCSBucketUtils.listFilesInBuild(job, buildNumber, searchPrefix)
					if err != nil || len(artifacts) == 0 {
						klog.Errorf("Error while looking for %s* in %s build %v: %v", searchPrefix, job, buildNumber, err)
						continue
					}
					for _, artifact := range artifacts {
						metricsFileName := filepath.Base(artifact)
						resultCategory := getResultCategory(metricsFileName, filePrefix, categoryLabel, artifacts)
						testDataResponse, err := g.GoogleGCSBucketUtils.getFileFromJenkinsGoogleBucket(job, buildNumber,
							fmt.Sprintf("artifacts/%v", metricsFileName))
						if err != nil {
							klog.Errorf("Error when reading response Body: %v", err)
							continue
						}
						buildData := getBuildData(result, tests.Prefix, resultCategory, testLabel, job, resultLock)
						testDescription.Parser(testDataResponse, buildNumber, buildData)
					}
					break
				}
			}
		}
	}
}

func getResultCategory(metricsFileName string, filePrefix string, category string, artifacts []string) string {
	if len(artifacts) <= 1 {
		return category
	}
	// If there are more artifacts, assume that this is a test suite run.
	trimmed := strings.TrimPrefix(metricsFileName, filePrefix+"_")
	suiteId := strings.Split(trimmed, "_")[0]
	return fmt.Sprintf("%v_%v", suiteId, category)
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

type bucketUtil struct {
	client  *storage.Client
	bucket  *storage.BucketHandle
	logPath string
}

func newBucketUtil(bucket, path, credentialPath string) (*bucketUtil, error) {
	ctx := context.Background()
	authOpt := option.WithoutAuthentication()
	if credentialPath != "" {
		authOpt = option.WithCredentialsFile(credentialPath)
	}
	c, err := storage.NewClient(ctx, authOpt)
	if err != nil {
		return nil, err
	}
	b := c.Bucket(bucket)
	return &bucketUtil{
		client:  c,
		bucket:  b,
		logPath: path,
	}, nil
}

func (b *bucketUtil) getBuildNumbersFromJenkinsGoogleBucket(job string) ([]int, error) {
	var builds []int
	ctx := context.Background()
	jobPrefix := joinStringsAndInts(b.logPath, job) + "/"
	klog.Infof("%s", jobPrefix)
	it := b.bucket.Objects(ctx, &storage.Query{
		Prefix:    jobPrefix,
		Delimiter: "/",
	})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if attrs.Prefix == "" {
			continue
		}
		build := strings.TrimPrefix(attrs.Prefix, jobPrefix)
		build = strings.TrimSuffix(build, "/")
		buildNo, err := strconv.Atoi(build)
		if err != nil {
			return nil, fmt.Errorf("unknown build name convention: %s", build)
		}
		builds = append(builds, buildNo)
	}
	return builds, nil
}

func (b *bucketUtil) listFilesInBuild(job string, buildNumber int, prefix string) ([]string, error) {
	var files []string
	ctx := context.Background()
	jobPrefix := joinStringsAndInts(b.logPath, job, buildNumber, prefix)
	it := b.bucket.Objects(ctx, &storage.Query{
		Prefix: jobPrefix,
	})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		files = append(files, attrs.Name)
	}
	return files, nil
}

func (b *bucketUtil) getFileFromJenkinsGoogleBucket(job string, buildNumber int, path string) ([]byte, error) {
	ctx := context.Background()
	filePath := joinStringsAndInts(b.logPath, job, buildNumber, path)
	rc, err := b.bucket.Object(filePath).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return data, nil
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
