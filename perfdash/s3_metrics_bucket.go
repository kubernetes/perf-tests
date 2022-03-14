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
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"k8s.io/klog"
)

// S3MetricsBucket that creates a AWS S3 client to fetch data.
type S3MetricsBucket struct {
	client  *s3.S3
	bucket  *string
	logPath string
}

// NewS3MetricsBucket creates a new S3MetricsBucket.
func NewS3MetricsBucket(bucket, pathPrefix, region string) (MetricsBucket, error) {
	return &S3MetricsBucket{
		client: s3.New(session.Must(session.NewSession(&aws.Config{
			Region:                        aws.String(region),
			CredentialsChainVerboseErrors: aws.Bool(true),
		}))),
		bucket:  aws.String(bucket),
		logPath: pathPrefix,
	}, nil
}

// GetBuildNumbers fetches the build numbers from an S3 Bucket.
func (s *S3MetricsBucket) GetBuildNumbers(job string) ([]int, error) {
	var builds []int
	jobPrefix := joinStringsAndInts(s.logPath, job) + "/"
	klog.Infof("%s", jobPrefix)

	objects, err := s.client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket:    s.bucket,
		Prefix:    aws.String(jobPrefix),
		Delimiter: aws.String("/"),
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

	if err := s.client.ListObjectsV2Pages(&s3.ListObjectsV2Input{Bucket: s.bucket, Prefix: aws.String(jobPrefix)},
		func(objects *s3.ListObjectsV2Output, lastPage bool) bool {
			for _, key := range objects.Contents {
				files = append(files, *key.Key)
			}
			return true
		}); err != nil {
		return nil, fmt.Errorf("while listing objects %v", err.Error())
	}

	return files, nil
}

func (s *S3MetricsBucket) GetFilePrefix(job string, buildNumber int, prefix string) string {
	return joinStringsAndInts(s.logPath, job, buildNumber, prefix)
}

// ReadFile reads the file contents from the S3 bucket.
func (s *S3MetricsBucket) ReadFile(job string, buildNumber int, path string) ([]byte, error) {
	filePath := joinStringsAndInts(s.logPath, job, buildNumber, path)

	resp, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: s.bucket,
		Key:    aws.String(filePath),
	})
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
