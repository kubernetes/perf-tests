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
	"strconv"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"k8s.io/klog"
)

// GCSMetricsBucket that creates a Google Cloud Storage client to fetch data.
type GCSMetricsBucket struct {
	client  *storage.Client
	bucket  *storage.BucketHandle
	logPath string
}

// NewGCSMetricsBucket creates a new GCSMetricsBucket.
func NewGCSMetricsBucket(bucket, path, credentialPath string, useADC bool) (MetricsBucket, error) {
	var c *storage.Client
	var err error
	ctx := context.Background()
	if credentialPath != "" {
		authOpt := option.WithCredentialsFile(credentialPath)
		c, err = storage.NewClient(ctx, authOpt)
	} else {
		if useADC {
			c, err = storage.NewClient(ctx)
		} else {
			authOpt := option.WithoutAuthentication()
			c, err = storage.NewClient(ctx, authOpt)
		}
	}
	if err != nil {
		return nil, err
	}
	b := c.Bucket(bucket)
	return &GCSMetricsBucket{
		client:  c,
		bucket:  b,
		logPath: path,
	}, nil
}

// GetBuildNumbers fetches the build numbers from a GCS Bucket.
func (b *GCSMetricsBucket) GetBuildNumbers(job string) ([]int, error) {
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

// ListFilesInBuild fetches the files in the build from GCS.
func (b *GCSMetricsBucket) ListFilesInBuild(job string, buildNumber int, prefix string) ([]string, error) {
	var files []string
	ctx := context.Background()
	jobPrefix := joinStringsAndInts(b.logPath, job, buildNumber, prefix)
	query := &storage.Query{
		Prefix: jobPrefix,
	}
	err := query.SetAttrSelection([]string{"Name"})
	if err != nil {
		return nil, err
	}

	it := b.bucket.Objects(ctx, query)
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

func (b *GCSMetricsBucket) GetFilePrefix(job string, buildNumber int, prefix string) string {
	return joinStringsAndInts(b.logPath, job, buildNumber, prefix)
}

// ReadFile reads the file contents from the GCS bucket.
func (b *GCSMetricsBucket) ReadFile(job string, buildNumber int, path string) ([]byte, error) {
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
