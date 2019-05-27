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
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"k8s.io/kubernetes/test/e2e/perftype"
)

const (
	// KubekinsBucket is a default kubekins bucket name.
	KubekinsBucket = "kubernetes-jenkins"
	// LogDir is a default name for kubekins log directory.
	LogDir = "logs"
)

// GoogleGCSDownloader that gets data about Google results from the GCS repository
type GoogleGCSDownloader struct {
	DefaultBuildsCount   int
	GoogleGCSBucketUtils *bucketUtil
	ConfigPaths          []string
}

// NewGoogleGCSDownloader creates a new GoogleGCSDownloader
func NewGoogleGCSDownloader(configPaths []string, defaultBuildsCount int) (*GoogleGCSDownloader, error) {
	b, err := newBucketUtil()
	if err != nil {
		return nil, err
	}
	return &GoogleGCSDownloader{
		DefaultBuildsCount:   defaultBuildsCount,
		GoogleGCSBucketUtils: b,
		ConfigPaths:          configPaths,
	}, nil
}

// TODO(random-liu): Only download and update new data each time.
func (g *GoogleGCSDownloader) getData() (JobToCategoryData, error) {
	newJobs, err := getProwConfig(g.ConfigPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh config: %v", err)
	}
	fmt.Print("Getting Data from GCS...\n")
	result := make(JobToCategoryData)
	var resultLock sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(newJobs))
	for job, tests := range newJobs {
		if tests.Prefix == "" {
			return nil, fmt.Errorf("Invalid empty Prefix for job %s", job)
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
	buildNumbers, err := g.GoogleGCSBucketUtils.getBuildNumbersFromJenkinsGoogleBucket(job)
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
					artifacts, err := g.GoogleGCSBucketUtils.listFilesInBuild(job, buildNumber, fileStem)
					if err != nil || len(artifacts) == 0 {
						fmt.Printf("Error while looking for %s* in %s build %v: %v\n", fileStem, job, buildNumber, err)
						continue
					}
					metricsFilename := artifacts[0][strings.LastIndex(artifacts[0], "/")+1:]
					if len(artifacts) > 1 {
						fmt.Printf("WARNING: found multiple %s files with data, reading only one: %s\n", fileStem, metricsFilename)
					}

					testDataResponse, err := g.GoogleGCSBucketUtils.getFileFromJenkinsGoogleBucket(job, buildNumber, fmt.Sprintf("artifacts/%v", metricsFilename))
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error when reading response Body: %v\n", err)
						continue
					}
					resultLock.Lock()
					buildData := result[tests.Prefix][categoryLabel][testLabel]
					resultLock.Unlock()
					testDescription.Parser(testDataResponse, buildNumber, buildData)
					break
				}
			}
		}
	}
}

type bucketUtil struct {
	client *storage.Client
	bucket *storage.BucketHandle
}

func newBucketUtil() (*bucketUtil, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx, option.WithoutAuthentication())
	if err != nil {
		return nil, err
	}
	bucket := client.Bucket(KubekinsBucket)
	return &bucketUtil{
		client: client,
		bucket: bucket,
	}, nil
}

func (b *bucketUtil) getBuildNumbersFromJenkinsGoogleBucket(job string) ([]int, error) {
	var builds []int
	ctx := context.Background()
	jobPrefix := joinStringsAndInts(LogDir, job) + "/"
	fmt.Printf("%s\n", jobPrefix)
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
	jobPrefix := joinStringsAndInts(LogDir, job, buildNumber, prefix)
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
	filePath := joinStringsAndInts(LogDir, job, buildNumber, path)
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
