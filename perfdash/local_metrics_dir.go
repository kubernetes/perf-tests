/*
Copyright 2024 The Kubernetes Authors.

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
)

// LocalMetricsDirectory prepares a client to fetch metrics data from a local directory.
// This is meant to be used for testing and development to load results from the report-dir output of clusterloader2.
type LocalMetricsDirectory struct {
	dirPath string
}

const filePathSeparator = string(os.PathSeparator)

// NewLocalMetricsDir creates a new LocalMetricsDirectory.
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
	filePath := b.dirPath + filePathSeparator + job
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

// ListFilesInBuild recursively fetches the files in the build from a local artifacts directory.
// Note: artifacts are expected to be in dirPath such in the format of dirPath/job/buildNumber.
// For example, it could be in artifacts/my-job/1234567890/artifacts.
func (b *LocalMetricsDirectory) ListFilesInBuild(job string, buildNumber int, prefix string) ([]string, error) {
	filePath := joinStringsAndInts(b.dirPath, filePathSeparator, job, filePathSeparator, buildNumber, prefix)

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
	p := joinStringsAndInts(b.dirPath, filePathSeparator, job, filePathSeparator, buildNumber, prefix)
	return p
}

// ReadFile reads the file contents from a local artifacts directory.
func (b *LocalMetricsDirectory) ReadFile(job string, buildNumber int, path string) ([]byte, error) {
	filePath := joinStringsAndInts(b.dirPath, filePathSeparator, job, filePathSeparator, buildNumber, path)

	return os.ReadFile(filePath)
}
