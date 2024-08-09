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
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"k8s.io/klog"
)

// LocalMetricsDirectory prepares a client to fetch metrics data from a local directory.
type LocalMetricsDirectory struct {
	dirPath string
}

// NewLocalMetricsDirectory creates a new LocalMetricsDirectory.
func NewLocalMetricsDir(pathPrefix string) (MetricsBucket, error) {
	absPath, err := filepath.Abs(pathPrefix)
	if err != nil {
		return nil, err
	}

	return &LocalMetricsDirectory{
		dirPath: absPath,
	}, nil
}

// GetBuildNumbers fetches the build numbers from a local artifacts directory.
func (b *LocalMetricsDirectory) GetBuildNumbers(job string) ([]int, error) {
	filePath := b.dirPath + "/" + job
	files, err := os.ReadDir(filePath)
	if err != nil {
		return nil, err
	}

	var builds []int
	for _, file := range files {
		if file.IsDir() {
			build, err := strconv.Atoi(file.Name())
			if err != nil {
				return nil, err
			}

			builds = append(builds, build)
		}
	}

	return builds, nil
}

// ListFilesInBuild fetches the files in the build from a local artifacts directory.
func (b *LocalMetricsDirectory) ListFilesInBuild(job string, buildNumber int, prefix string) ([]string, error) {
	filePath := joinStringsAndInts(b.dirPath, "/", job, "/", buildNumber, prefix)
	klog.Infof("Listing files in %s", filePath)

	files := []string{}
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}

		return nil
	}

	err := filepath.WalkDir(filePath, walkFn)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (b *LocalMetricsDirectory) GetFilePrefix(job string, buildNumber int, prefix string) string {
	p := joinStringsAndInts(b.dirPath, "/", job, "/", buildNumber, prefix)
	// klog.Infof("Prefix: %s", p)
	return p
}

// ReadFile reads the file contents from a local artifacts directory.
func (b *LocalMetricsDirectory) ReadFile(job string, buildNumber int, path string) ([]byte, error) {
	filePath := joinStringsAndInts(b.dirPath, "/", job, "/", buildNumber, path)

	return os.ReadFile(filePath)
}
