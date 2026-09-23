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
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
	err = ingester.IngestBuild(context.Background(), "ci-kubernetes-benchmark-write-throughput", 101, categoryMap, buildTime)
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

	// Test ThanosClient mock server (exercises fallback to /api/v1/series when /api/v1/label/... returns 404)
	mockThanosServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/series" {
			match := r.URL.Query().Get("match[]")
			if match == `{__name__="perfdash_metric"}` {
				resp := promSeriesResponse{
					Status: "success",
					Data: []map[string]string{
						{"__name__": "perfdash_metric", "job": "ci-kubernetes-benchmark-write-throughput", "category": "APIServer", "metric": "Latency"},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			if match == `{__name__="perfdash_metric",job="ci-kubernetes-benchmark-write-throughput"}` {
				resp := promSeriesResponse{
					Status: "success",
					Data: []map[string]string{
						{"__name__": "perfdash_metric", "job": "ci-kubernetes-benchmark-write-throughput", "category": "APIServer"},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			if match == `{__name__="perfdash_metric",job="ci-kubernetes-benchmark-write-throughput",category="APIServer"}` {
				resp := promSeriesResponse{
					Status: "success",
					Data: []map[string]string{
						{"__name__": "perfdash_metric", "job": "ci-kubernetes-benchmark-write-throughput", "category": "APIServer", "metric": "Latency"},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
		}

		if r.URL.Path == "/api/v1/query" {
			query := r.URL.Query().Get("query")
			if query == `last_over_time(perfdash_metric{job="ci-kubernetes-benchmark-write-throughput",category="APIServer",metric="Latency"}[10y])` {
				resp := promQueryResponse{
					Status: "success",
				}
				resp.Data.ResultType = "vector"
				resp.Data.Result = []struct {
					Metric map[string]string `json:"metric"`
					Value  []interface{}     `json:"value"`
				}{
					{
						Metric: map[string]string{
							"__name__":    "perfdash_metric",
							"job":         "ci-kubernetes-benchmark-write-throughput",
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
				json.NewEncoder(w).Encode(resp)
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
	assert.Equal(t, []string{"ci-kubernetes-benchmark-write-throughput"}, jobNames)

	// Test /metriccategorynames
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metriccategorynames?jobname=ci-kubernetes-benchmark-write-throughput", nil)
	client.ServeCategoryNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var categories []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &categories))
	assert.Equal(t, []string{"APIServer"}, categories)

	// Test /metricnames
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metricnames?jobname=ci-kubernetes-benchmark-write-throughput&metriccategoryname=APIServer", nil)
	client.ServeMetricNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var metricNames []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metricNames))
	assert.Equal(t, []string{"Latency"}, metricNames)

	// Test /buildsdata
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/buildsdata?jobname=ci-kubernetes-benchmark-write-throughput&metriccategoryname=APIServer&metricname=Latency", nil)
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

	// Verify that TSDB block was generated and no .staging-block-* directory remains
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
	assert.Equal(t, 1, blockCount, "expected exactly 1 TSDB block created by Downloader with ThanosIngester")

	// Simulate Perfdash process restart: a fresh ThanosIngester + Downloader pointing at the same TSDB directory
	// must recognize build 101 from on-disk meta.json and skip writing a duplicate block.
	restartedIngester := NewThanosIngester(tempTSDB)
	assert.True(t, restartedIngester.HasBuild("benchmark etcd write throughput", 101))
	assert.True(t, restartedIngester.HasBuild(job, 101))

	restartedDownloader := NewDownloader(&DownloaderOptions{DefaultBuildsCount: 1}, bucket, false)
	restartedDownloader.SetThanosIngester(restartedIngester)
	wg.Add(1)
	restartedDownloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "benchmark etcd write throughput",
		Descriptions: performanceDescriptions,
		BuildsCount:  1,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	entriesAfterRestart, err := os.ReadDir(tempTSDB)
	require.NoError(t, err)
	assert.Len(t, entriesAfterRestart, len(entries), "restarted Downloader must not write duplicate TSDB blocks")
}

func TestAdversarialDuplicatesAndSpecialLabels(t *testing.T) {
	tempTSDB, err := os.MkdirTemp("", "thanos_adversarial_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempTSDB)

	ingester := NewThanosIngester(tempTSDB)
	categoryMap := make(CategoryToMetricData)
	categoryMap["APIServer"] = make(MetricToBuildData)
	categoryMap["APIServer"]["Responsiveness"] = &BuildData{
		Job:     "ci-kubernetes-e2e-gci-gce-scalability",
		Version: "v1",
		Builds: NewBuilds(map[string][]perftype.DataItem{
			"202": {
				{
					Unit: "ms",
					Labels: map[string]string{
						"Resource":        "pods",
						"Subresource":     "",
						"Verb":            "LIST",
						"Scope":           "cluster",
						"job":             "kube-apiserver",
						"test-phase/name": "load-phase-1",
					},
					Data: map[string]float64{
						"Perc50": 15.2,
						"Perc99": 120.5,
						"NaNVal": math.NaN(),
						"InfVal": math.Inf(1),
					},
				},
				// Exact duplicate label set in the same build (previously triggered tsdb.ErrDuplicateSampleForTimestamp)
				{
					Unit: "ms",
					Labels: map[string]string{
						"Resource":        "pods",
						"Subresource":     "",
						"Verb":            "LIST",
						"Scope":           "cluster",
						"job":             "kube-apiserver",
						"test-phase/name": "load-phase-1",
					},
					Data: map[string]float64{
						"Perc50": 19.9,
						"Perc99": 140.0,
					},
				},
			},
		}),
	}

	err = ingester.IngestBuild(context.Background(), "gce-5000Nodes", 202, categoryMap, time.Now().Add(-1*time.Hour).UTC())
	require.NoError(t, err, "IngestBuild must not fail on duplicate series, NaN/Inf values, reserved label names, or special characters")
}

func freeLocalPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func findThanosBinary() string {
	if p, err := exec.LookPath("thanos"); err == nil {
		return p
	}
	fallback := "/usr/local/google/home/jying/gospace/bin/thanos"
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

func TestLiveThanosStoreAndQueryEndToEnd(t *testing.T) {
	thanosBin := findThanosBinary()
	if thanosBin == "" {
		t.Skip("thanos binary not found; skipping live Thanos Store + Query integration test")
	}

	tempDir, err := os.MkdirTemp("", "thanos_live_e2e_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tsdbDir := filepath.Join(tempDir, "prometheus")
	cacheDir := filepath.Join(tempDir, "cache")
	require.NoError(t, os.MkdirAll(tsdbDir, 0755))
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	bucketConfigPath := filepath.Join(tempDir, "bucket.yml")
	bucketYAML := fmt.Sprintf("type: FILESYSTEM\nconfig:\n  directory: %q\n", tsdbDir)
	require.NoError(t, os.WriteFile(bucketConfigPath, []byte(bucketYAML), 0644))

	ingester := NewThanosIngester(tsdbDir)

	// Ingest two builds (101 and 102) under job prefix "gce-5000Nodes" backed by Prow job "ci-kubernetes-e2e-gci-gce-scalability"
	for _, buildNum := range []int{101, 102} {
		buildStr := fmt.Sprintf("%d", buildNum)
		categoryMap := make(CategoryToMetricData)
		categoryMap["APIServer"] = make(MetricToBuildData)
		categoryMap["APIServer"]["LoadResponsiveness_PrometheusSimple"] = &BuildData{
			Job:     "ci-kubernetes-e2e-gci-gce-scalability",
			Version: "v1",
			Builds: NewBuilds(map[string][]perftype.DataItem{
				buildStr: {
					{
						Unit: "ms",
						Labels: map[string]string{
							"Resource":        "pods",
							"Subresource":     "",
							"Verb":            "LIST",
							"Scope":           "cluster",
							"job":             "kube-apiserver",
							"test-phase/name": "load",
						},
						Data: map[string]float64{
							"Perc50": float64(100 + buildNum),
							"Perc90": float64(200 + buildNum),
							"Perc99": float64(300 + buildNum),
						},
					},
					{
						Unit: "ms",
						Labels: map[string]string{
							"Resource":        "pods",
							"Subresource":     "status",
							"Verb":            "PATCH",
							"Scope":           "resource",
							"job":             "kube-apiserver",
							"test-phase/name": "load",
						},
						Data: map[string]float64{
							"Perc50": 5.5,
							"Perc90": 12.0,
							"Perc99": 25.0,
						},
					},
				},
			}),
		}
		buildTS := time.Now().Add(-time.Duration(200-buildNum) * time.Minute).UTC()
		require.NoError(t, ingester.IngestBuild(context.Background(), "gce-5000Nodes", buildNum, categoryMap, buildTS))
	}

	storeGRPCPort := freeLocalPort(t)
	storeHTTPPort := freeLocalPort(t)
	queryGRPCPort := freeLocalPort(t)
	queryHTTPPort := freeLocalPort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storeCmd := exec.CommandContext(ctx, thanosBin,
		"store",
		"--objstore.config-file="+bucketConfigPath,
		"--data-dir="+cacheDir,
		"--sync-block-duration=1s",
		fmt.Sprintf("--grpc-address=127.0.0.1:%d", storeGRPCPort),
		fmt.Sprintf("--http-address=127.0.0.1:%d", storeHTTPPort),
	)
	require.NoError(t, storeCmd.Start())
	defer func() {
		_ = storeCmd.Process.Kill()
		_ = storeCmd.Wait()
	}()

	queryCmd := exec.CommandContext(ctx, thanosBin,
		"query",
		fmt.Sprintf("--http-address=127.0.0.1:%d", queryHTTPPort),
		fmt.Sprintf("--grpc-address=127.0.0.1:%d", queryGRPCPort),
		fmt.Sprintf("--endpoint=127.0.0.1:%d", storeGRPCPort),
	)
	require.NoError(t, queryCmd.Start())
	defer func() {
		_ = queryCmd.Process.Kill()
		_ = queryCmd.Wait()
	}()

	queryURL := fmt.Sprintf("http://127.0.0.1:%d", queryHTTPPort)
	client := NewThanosClient(queryURL)

	// Poll until Thanos Query discovers Thanos Store and sees the "gce-5000Nodes" job
	var jobNames []string
	require.Eventually(t, func() bool {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/jobnames", nil)
		client.ServeJobNames(rec, req)
		if rec.Code != http.StatusOK {
			return false
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &jobNames)
		return len(jobNames) == 1 && jobNames[0] == "gce-5000Nodes"
	}, 15*time.Second, 200*time.Millisecond, "expected live Thanos Query to return ingested job 'gce-5000Nodes'")

	// Verify /metriccategorynames by jobPrefix ("gce-5000Nodes") and by prow_job ("ci-kubernetes-e2e-gci-gce-scalability")
	for _, queryJob := range []string{"gce-5000Nodes", "ci-kubernetes-e2e-gci-gce-scalability"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/metriccategorynames?jobname="+queryJob, nil)
		client.ServeCategoryNames(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var categories []string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &categories))
		assert.Equal(t, []string{"APIServer"}, categories)
	}

	// Verify /metricnames
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metricnames?jobname=gce-5000Nodes&metriccategoryname=APIServer", nil)
	client.ServeMetricNames(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var metrics []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metrics))
	assert.Equal(t, []string{"LoadResponsiveness_PrometheusSimple"}, metrics)

	// Verify /buildsdata end-to-end reconstruction
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/buildsdata?jobname=gce-5000Nodes&metriccategoryname=APIServer&metricname=LoadResponsiveness_PrometheusSimple", nil)
	client.ServeBuildsData(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var buildsData BuildData
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &buildsData))
	assert.Equal(t, "ci-kubernetes-e2e-gci-gce-scalability", buildsData.Job, "BuildData.Job must resolve to the underlying Prow GCS job name")
	assert.Equal(t, "v1", buildsData.Version)

	items101 := buildsData.Builds.Builds("101")
	require.Len(t, items101, 2, "expected both empty-Subresource LIST item and status-Subresource PATCH item")
	assert.Equal(t, "", items101[0].Labels["Subresource"])
	assert.Equal(t, "LIST", items101[0].Labels["Verb"])
	assert.Equal(t, "kube-apiserver", items101[0].Labels["job"], "reserved 'job' label in item.Labels must be decoded intact")
	assert.Equal(t, "load", items101[0].Labels["test-phase/name"], "special characters in item.Labels key must be decoded intact")
	assert.Equal(t, 401.0, items101[0].Data["Perc99"])

	assert.Equal(t, "status", items101[1].Labels["Subresource"])
	assert.Equal(t, "PATCH", items101[1].Labels["Verb"])
	assert.Equal(t, 25.0, items101[1].Data["Perc99"])

	items102 := buildsData.Builds.Builds("102")
	require.Len(t, items102, 2)
	assert.Equal(t, 402.0, items102[0].Data["Perc99"])
}

