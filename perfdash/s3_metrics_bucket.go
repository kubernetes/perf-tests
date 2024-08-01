/*
Copyright 2020 The Kubernetes Authors.

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
	"strconv"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"k8s.io/klog"
	"k8s.io/utils/ptr"
)

// S3MetricsBucket that creates a AWS S3 client to fetch data.
type S3MetricsBucket struct {
	client  *s3.Client
	bucket  *string
	logPath string
}

// NewS3MetricsBucket creates a new S3MetricsBucket.
func NewS3MetricsBucket(bucket, pathPrefix, region string) (MetricsBucket, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, err
	}
	return &S3MetricsBucket{
		client:  s3.NewFromConfig(cfg),
		bucket:  ptr.To(bucket),
		logPath: pathPrefix,
	}, nil
}

// GetBuildNumbers fetches the build numbers from an S3 Bucket.
func (s *S3MetricsBucket) GetBuildNumbers(job string) ([]int, error) {
	var builds []int
	jobPrefix := joinStringsAndInts(s.logPath, job) + "/"
	klog.Infof("%s", jobPrefix)

	objects, err := s.client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket:    s.bucket,
		Prefix:    ptr.To(jobPrefix),
		Delimiter: ptr.To("/"),
	})
	if err != nil {
		return nil, err
	}

	for _, key := range objects.CommonPrefixes {
		build := *key.Prefix
		build = strings.TrimPrefix(build, jobPrefix)
		build = strings.TrimSuffix(build, "/")

		buildNo, err := strconv.Atoi(build)
		if err != nil {
			return nil, fmt.Errorf("unknown build name convention: %s", build)
		}
		builds = append(builds, buildNo)
	}

	return builds, nil
}

// ListFilesInBuild fetches the files in the build from S3.
func (s *S3MetricsBucket) ListFilesInBuild(job string, buildNumber int, prefix string) ([]string, error) {
	var files []string
	jobPrefix := joinStringsAndInts(s.logPath, job, buildNumber, prefix)

	result, err := s.client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket: s.bucket,
		Prefix: ptr.To(jobPrefix),
	})
	if err != nil {
		return nil, fmt.Errorf("while listing objects %v", err.Error())
	}
	for _, item := range result.Contents {
		files = append(files, *item.Key)
	}

	return files, nil
}

func (s *S3MetricsBucket) GetFilePrefix(job string, buildNumber int, prefix string) string {
	return joinStringsAndInts(s.logPath, job, buildNumber, prefix)
}

// ReadFile reads the file contents from the S3 bucket.
func (s *S3MetricsBucket) ReadFile(job string, buildNumber int, path string) ([]byte, error) {
	filePath := joinStringsAndInts(s.logPath, job, buildNumber, path)

	resp, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: s.bucket,
		Key:    ptr.To(filePath),
	})
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
