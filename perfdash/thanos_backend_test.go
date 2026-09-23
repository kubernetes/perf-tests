/*
Copyright The Kubernetes Authors.

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/kubernetes/test/e2e/perftype"
)

func TestThanosIngestAndClientServing(t *testing.T) {
	tempTSDB, err := os.MkdirTemp("", "thanos_tsdb_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempTSDB)

	ingester := NewThanosIngester(tempTSDB)

	categoryMap := make(CategoryToMetricData)
	categoryMap["APIServer"] = make(MetricToBuildData)
	categoryMap["APIServer"]["Latency"] = &BuildData{
		Job:     "ci-kubernetes-benchmark-write-throughput",
		Version: "v1",
		Builds: NewBuilds(map[string][]perftype.DataItem{
			"101": {
				{
					Unit: "ms",
					Labels: map[string]string{
						"Resource":    "pods",
						"Subresource": "",
						"Verb":        "POST",
						"Scope":       "resource",
					},
					Data: map[string]float64{
						"Perc50": 12.5,
						"Perc90": 25.0,
						"Perc99": 50.0,
					},
				},
			},
		}),
	}

	buildTime := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	err = ingester.IngestBuild(context.Background(), "benchmark-write-throughput", 101, categoryMap, buildTime)
	require.NoError(t, err)

	// Verify TSDB block directory was created
	entries, err := os.ReadDir(tempTSDB)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	foundMeta := false
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			metaFile := filepath.Join(tempTSDB, entry.Name(), "meta.json")
			if _, err := os.Stat(metaFile); err == nil {
				foundMeta = true
				break
			}
		}
	}
	assert.True(t, foundMeta, "expected at least one TSDB block directory with meta.json")

	// Test ThanosClient against mock Thanos HTTP API
	mockThanosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		match := r.URL.Query().Get("match[]")
		switch r.URL.Path {
		case "/api/v1/label/job/values":
			if match == `{__name__="perfdash_metric"}` {
				_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
					Status: "success",
					Data:   []string{"benchmark-write-throughput"},
				})
				return
			}
		case "/api/v1/label/category/values":
			if match == `{__name__="perfdash_metric",job="benchmark-write-throughput"}` {
				_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
					Status: "success",
					Data:   []string{"APIServer"},
				})
				return
			}
		case "/api/v1/label/metric/values":
			if match == `{__name__="perfdash_metric",job="benchmark-write-throughput",category="APIServer"}` {
				_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
					Status: "success",
					Data:   []string{"Latency"},
				})
				return
			}
		case "/api/v1/query":
			query := r.URL.Query().Get("query")
			if query == `last_over_time(perfdash_metric{job="benchmark-write-throughput",category="APIServer",metric="Latency"}[10y])` {
				resp := promQueryResponse{Status: "success"}
				resp.Data.ResultType = "vector"
				resp.Data.Result = []struct {
					Metric map[string]string `json:"metric"`
					Value  []interface{}     `json:"value"`
				}{
					{
						Metric: map[string]string{
							"__name__":    "perfdash_metric",
							"job":         "benchmark-write-throughput",
							"prow_job":    "ci-kubernetes-benchmark-write-throughput",
							"category":    "APIServer",
							"metric":      "Latency",
							"build":       "101",
							"percentile":  "Perc99",
							"unit":        "ms",
							"Resource":    "pods",
							"Subresource": emptyLabelValueSentinel,
							"Verb":        "POST",
							"Scope":       "resource",
						},
						Value: []interface{}{1790180000.0, "50.0"},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer mockThanosServer.Close()

	client := NewThanosClient(mockThanosServer.URL)

	// Test /jobnames
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/jobnames", nil)
	client.ServeJobNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var jobNames []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobNames))
	assert.Equal(t, []string{"benchmark-write-throughput"}, jobNames)

	// Test /metriccategorynames
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metriccategorynames?jobname=benchmark-write-throughput", nil)
	client.ServeCategoryNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var categories []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &categories))
	assert.Equal(t, []string{"APIServer"}, categories)

	// Test /metricnames
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metricnames?jobname=benchmark-write-throughput&metriccategoryname=APIServer", nil)
	client.ServeMetricNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var metricNames []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metricNames))
	assert.Equal(t, []string{"Latency"}, metricNames)

	// Test /buildsdata
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/buildsdata?jobname=benchmark-write-throughput&metriccategoryname=APIServer&metricname=Latency", nil)
	client.ServeBuildsData(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var buildsData BuildData
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &buildsData))
	assert.Equal(t, "ci-kubernetes-benchmark-write-throughput", buildsData.Job)
	assert.Equal(t, "v1", buildsData.Version)
	items := buildsData.Builds.Builds("101")
	require.Len(t, items, 1)
	assert.Equal(t, "ms", items[0].Unit)
	assert.Equal(t, "pods", items[0].Labels["Resource"])
	subVal, hasSub := items[0].Labels["Subresource"]
	assert.True(t, hasSub, "expected empty Subresource label key to be preserved")
	assert.Equal(t, "", subVal)
	assert.Equal(t, 50.0, items[0].Data["Perc99"])
}

func TestDownloaderThanosIngestionIntegration(t *testing.T) {
	job := "ci-kubernetes-benchmark-etcd-write-throughput"
	artifactPath := "artifacts/GenericPrometheusQuery EtcdWriteThroughput_etcd-write-throughput_2026-09-17T06:53:53Z.json"
	startedPath := "started.json"

	bucket := &fakeMetricsBucket{
		builds: []int{101},
		files: map[string][]byte{
			joinStringsAndInts(job, 101, artifactPath): []byte(`{
				"version": "v1",
				"dataItems": [{
					"data": {"PutThroughput": 988.03, "BackendCommitRate": 6.44},
					"unit": "ops/s"
				}]
			}`),
			joinStringsAndInts(job, 101, startedPath): []byte(`{"timestamp": 1790180000}`),
		},
	}

	tempTSDB, err := os.MkdirTemp("", "thanos_downloader_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempTSDB)

	ingester := NewThanosIngester(tempTSDB)
	downloader := NewDownloader(&DownloaderOptions{DefaultBuildsCount: 1}, bucket, false)
	downloader.SetThanosIngester(ingester)

	result := make(JobToCategoryData)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(1)
	downloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "benchmark etcd write throughput",
		Descriptions: performanceDescriptions,
		BuildsCount:  1,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	entries, err := os.ReadDir(tempTSDB)
	require.NoError(t, err)
	blockCount := 0
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), ".staging-"), "staging directory should be cleaned up")
		if entry.IsDir() {
			metaFile := filepath.Join(tempTSDB, entry.Name(), "meta.json")
			if _, err := os.Stat(metaFile); err == nil {
				blockCount++
			}
		}
	}
	assert.Equal(t, 1, blockCount, "expected 1 TSDB block created by Downloader with ThanosIngester")

	// Verify a restarted ThanosIngester discovers the on-disk block and skips duplicate ingestion.
	restartedIngester := NewThanosIngester(tempTSDB)
	assert.True(t, restartedIngester.HasBuild("benchmark etcd write throughput", 101))
}


