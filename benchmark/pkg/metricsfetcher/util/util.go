/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"io/ioutil"

	"k8s.io/contrib/test-utils/utils"
)

// Allowed source modes for fetching the metrics
const (
	GCS = "gcs"
	// TODO(shyamjvs): Add a mode to fetch metrics locally, if needed.
)

// JobLogUtils provides the set of methods used by runselector and scraper
// for obtaining metrics for a given source mode (GCS/local/...).
type JobLogUtils interface {
	GetLatestBuildNumberForJob(string) (int, error)
	GetJobRunStartTimestamp(string, int) (uint64, error)
	GetJobRunFinishedStatus(string, int) (bool, error)
	GetJobRunBuildLogContents(string, int) ([]byte, error)
}

// GCSLogUtils defines JobLogUtils interface for the case when source of logs is GCS.
type GCSLogUtils struct {
	JobLogUtils
	googleGCSBucketUtils *utils.Utils
}

// NewGCSLogUtils returns new GCSLogUtils struct with GCS utils initialized.
func NewGCSLogUtils() GCSLogUtils {
	return GCSLogUtils{
		googleGCSBucketUtils: utils.NewUtils(utils.KubekinsBucket, utils.LogDir),
	}
}

// GetLatestBuildNumberForJob returns latest build number for the job.
func (utils GCSLogUtils) GetLatestBuildNumberForJob(job string) (int, error) {
	return utils.googleGCSBucketUtils.GetLastestBuildNumberFromJenkinsGoogleBucket(job)
}

// GetJobRunStartTimestamp returns start timestamp for the job run.
func (utils GCSLogUtils) GetJobRunStartTimestamp(job string, run int) (uint64, error) {
	startStatus, err := utils.googleGCSBucketUtils.CheckStartedStatus(job, run)
	if err != nil {
		return 0, err
	}
	return startStatus.Timestamp, nil
}

// GetJobRunFinishedStatus returns the finished status (true/false) for the job run.
func (utils GCSLogUtils) GetJobRunFinishedStatus(job string, run int) (bool, error) {
	return utils.googleGCSBucketUtils.CheckFinishedStatus(job, run)
}

// GetJobRunBuildLogContents returns the contents of the build log file for the job run.
func (utils GCSLogUtils) GetJobRunBuildLogContents(job string, run int) ([]byte, error) {
	response, err := utils.googleGCSBucketUtils.GetFileFromJenkinsGoogleBucket(job, run, "build-log.txt")
	if err != nil {
		return nil, fmt.Errorf("Couldn't read build log file from GCS: %v", err)
	}
	defer response.Body.Close()
	return ioutil.ReadAll(response.Body)
}

// GetJobLogUtilsForMode gives the right utils object based on the source mode.
func GetJobLogUtilsForMode(mode string) (JobLogUtils, error) {
	switch mode {
	case GCS:
		return NewGCSLogUtils(), nil
	default:
		return nil, fmt.Errorf("Unknown source mode '%v'", mode)
	}
}
