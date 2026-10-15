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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/tsdb"
	"github.com/prometheus/tsdb/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/kubernetes/test/e2e/perftype"
)

func TestThanosIngestAndClientServing(t *testing.T) {
	tempTSDB := t.TempDir()
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
	err := ingester.IngestBuild(context.Background(), "benchmark-write-throughput", 101, categoryMap, buildTime)
	require.NoError(t, err)

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

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/jobnames", nil)
	client.ServeJobNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var jobNames []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobNames))
	assert.Equal(t, []string{"benchmark-write-throughput"}, jobNames)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metriccategorynames?jobname=benchmark-write-throughput", nil)
	client.ServeCategoryNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var categories []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &categories))
	assert.Equal(t, []string{"APIServer"}, categories)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/metricnames?jobname=benchmark-write-throughput&metriccategoryname=APIServer", nil)
	client.ServeMetricNames(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var metricNames []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metricNames))
	assert.Equal(t, []string{"Latency"}, metricNames)

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

	// Verify missing required query parameters return 400 Bad Request
	for _, path := range []string{
		"/metriccategorynames",
		"/metricnames?jobname=benchmark-write-throughput",
		"/buildsdata?jobname=benchmark-write-throughput&metriccategoryname=APIServer",
	} {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", path, nil)
		switch {
		case strings.HasPrefix(path, "/metriccategorynames"):
			client.ServeCategoryNames(rec, req)
		case strings.HasPrefix(path, "/metricnames"):
			client.ServeMetricNames(rec, req)
		default:
			client.ServeBuildsData(rec, req)
		}
		assert.Equal(t, http.StatusBadRequest, rec.Code, "expected 400 for %s", path)
	}

	// Verify invalid build timestamp is rejected
	err = ingester.IngestBuild(context.Background(), "benchmark-write-throughput", 102, categoryMap, time.Time{})
	assert.Error(t, err)

	// Verify 0-sample build is marked ingested so it is not re-downloaded on subsequent syncs
	emptyCategoryMap := make(CategoryToMetricData)
	err = ingester.IngestBuild(context.Background(), "benchmark-write-throughput", 103, emptyCategoryMap, buildTime)
	require.NoError(t, err)
	assert.True(t, ingester.HasBuild("benchmark-write-throughput", 103))

	// Verify parsePromSampleValue rejects malformed sample tuples
	_, err = parsePromSampleValue([]interface{}{1790180000.0})
	assert.Error(t, err)
	_, err = parsePromSampleValue([]interface{}{1790180000.0, "not-a-float"})
	assert.Error(t, err)
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

	tempTSDB := t.TempDir()

	// Create an orphaned staging directory and verify Bootstrap removes it.
	orphanDir := filepath.Join(tempTSDB, ".staging-block-orphan")
	require.NoError(t, os.MkdirAll(orphanDir, 0755))

	ingester := NewThanosIngester(tempTSDB)
	require.NoError(t, ingester.Bootstrap())
	_, statErr := os.Stat(orphanDir)
	assert.True(t, os.IsNotExist(statErr), "expected Bootstrap to remove orphaned staging directory")

	downloader := NewDownloader(&DownloaderOptions{DefaultBuildsCount: 1}, bucket, false, ingester)

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
	require.NoError(t, restartedIngester.Bootstrap())
	assert.True(t, restartedIngester.HasBuild("benchmark etcd write throughput", 101))

	// Ingest builds 102 and 103 with BuildsCount=2 and verify build 101 is pruned during compaction.
	bucket.builds = []int{101, 102, 103}
	for _, b := range []int{102, 103} {
		bucket.files[joinStringsAndInts(job, b, artifactPath)] = bucket.files[joinStringsAndInts(job, 101, artifactPath)]
	}
	bucket.files[joinStringsAndInts(job, 102, startedPath)] = []byte(`{"timestamp": 1790183600}`)
	bucket.files[joinStringsAndInts(job, 103, startedPath)] = []byte(`{"timestamp": 1790187200}`)

	wg.Add(1)
	downloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "benchmark etcd write throughput",
		Descriptions: performanceDescriptions,
		BuildsCount:  2,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	assert.False(t, ingester.HasBuild("benchmark etcd write throughput", 101), "expired build 101 should be pruned")
	assert.True(t, ingester.HasBuild("benchmark etcd write throughput", 102))
	assert.True(t, ingester.HasBuild("benchmark etcd write throughput", 103))

	// Now ingest build 104 into the already-compacted 2-build block [102, 103] with BuildsCount=2,
	// exercising partial-block pruning (deleteBuildsFromBlock on [102, 103] to drop 102 while keeping 103).
	bucket.builds = []int{101, 102, 103, 104}
	bucket.files[joinStringsAndInts(job, 104, artifactPath)] = bucket.files[joinStringsAndInts(job, 101, artifactPath)]
	bucket.files[joinStringsAndInts(job, 104, startedPath)] = []byte(`{"timestamp": 1790190800}`)

	wg.Add(1)
	downloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "benchmark etcd write throughput",
		Descriptions: performanceDescriptions,
		BuildsCount:  2,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	assert.False(t, ingester.HasBuild("benchmark etcd write throughput", 102), "expired build 102 should be pruned from compacted block")
	assert.True(t, ingester.HasBuild("benchmark etcd write throughput", 103))
	assert.True(t, ingester.HasBuild("benchmark etcd write throughput", 104))

	entries, err = os.ReadDir(tempTSDB)
	require.NoError(t, err)
	var compactedDir string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			compactedDir = filepath.Join(tempTSDB, entry.Name())
		}
	}
	require.NotEmpty(t, compactedDir)
	meta, err := readBlockMeta(compactedDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"103", "104"}, meta.Thanos.Builds)

	pb, err := tsdb.OpenBlock(log.NewNopLogger(), compactedDir, nil)
	require.NoError(t, err)
	defer pb.Close()
	q, err := tsdb.NewBlockQuerier(pb, pb.MinTime(), pb.MaxTime())
	require.NoError(t, err)
	defer q.Close()
	ss, err := q.Select(labels.NewEqualMatcher("build", "102"))
	require.NoError(t, err)
	assert.False(t, ss.Next(), "expected 0 series for pruned build 102 in compacted TSDB block")
	ss104, err := q.Select(labels.NewEqualMatcher("build", "104"))
	require.NoError(t, err)
	assert.True(t, ss104.Next(), "expected series for retained build 104 in compacted TSDB block")
}

func TestLabelEncodingRoundTripAndConcurrentCompaction(t *testing.T) {
	item := perftype.DataItem{
		Unit: "ms",
		Labels: map[string]string{
			"job":               "etcd",
			"invalid-prom-name": "val1",
			"perfdash_lbl_raw":  "val2",
			"Subresource":       "",
		},
		Data: map[string]float64{"Perc50": 1.0},
	}
	encoded := encodeSeriesLabels("gce-5000Nodes", "ci-kubernetes-e2e-gce-scale-performance-5000", "APIServer", "Latency", "101", "Perc50", item)
	decoded := decodeSeriesLabels(encoded.Map())
	assert.Equal(t, item.Labels, decoded)

	// Verify concurrent IngestBuild + CompactJobBlocks for the same jobPrefix is race-free and leaves 1 block.
	tempTSDB := t.TempDir()
	ingester := NewThanosIngester(tempTSDB)

	var wg sync.WaitGroup
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go func(buildNum int) {
			defer wg.Done()
			bStr := fmt.Sprintf("%d", buildNum)
			catMap := CategoryToMetricData{
				"Cat": MetricToBuildData{
					"Met": &BuildData{
						Job: "prow-job",
						Builds: NewBuilds(map[string][]perftype.DataItem{
							bStr: {{Unit: "s", Data: map[string]float64{"Perc99": float64(buildNum)}}},
						}),
					},
				},
			}
			ts := time.Unix(int64(1790000000+buildNum*3600), 0)
			require.NoError(t, ingester.IngestBuild(context.Background(), "shared-prefix", buildNum, catMap, ts))
			require.NoError(t, ingester.CompactJobBlocks(context.Background(), "shared-prefix", 3))
		}(i)
	}
	wg.Wait()

	entries, err := os.ReadDir(tempTSDB)
	require.NoError(t, err)
	blocks := 0
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			blocks++
		}
	}
	assert.Equal(t, 1, blocks, "expected concurrent compactions on shared prefix to produce 1 block")
}

func TestPrometheusProxyHandler(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[string]int)

	mockThanos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		match := r.URL.Query().Get("match[]")
		switch r.URL.Path {
		case "/api/v1/label/job/values":
			assert.Equal(t, "", match)
			_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
				Status: "success",
				Data:   []string{"gce-5000Nodes", "gce-100Nodes"},
			})
			return
		case "/api/v1/label/category/values":
			if match == `perfdash_metric{job="gce-5000Nodes"}` {
				_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
					Status: "success",
					Data:   []string{"APIServer", "E2E"},
				})
				return
			}
		case "/api/v1/label/metric/values":
			if match == `perfdash_metric{job="gce-5000Nodes",category="APIServer"}` {
				_ = json.NewEncoder(w).Encode(promLabelValuesResponse{
					Status: "success",
					Data:   []string{"LoadResponsiveness_PrometheusSimple"},
				})
				return
			}
		case "/api/v1/query":
			q := r.URL.Query().Get("query")
			if q == `count without (build, prow_job) (last_over_time(perfdash_metric{job="gce-5000Nodes",category="APIServer",metric="LoadResponsiveness_PrometheusSimple"}[10y]))` {
				resp := promQueryResponse{Status: "success"}
				resp.Data.ResultType = "vector"
				resp.Data.Result = []struct {
					Metric map[string]string `json:"metric"`
					Value  []interface{}     `json:"value"`
				}{
					{
						Metric: map[string]string{
							"job":        "gce-5000Nodes",
							"category":   "APIServer",
							"metric":     "LoadResponsiveness_PrometheusSimple",
							"Resource":   "pods",
							"Verb":       "WATCHLIST",
							"percentile": "Perc99",
							"unit":       "ms",
						},
						Value: []interface{}{1790180000.0, "43"},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		case "/api/v1/series":
			if match == `apiserver_request_total{run="run-123"}` {
				_ = json.NewEncoder(w).Encode(promSeriesResponse{
					Status: "success",
					Data: []map[string]string{
						{"__name__": "apiserver_request_total", "resource": "pods", "verb": "GET"},
					},
				})
				return
			}
		case "/api/v1/query_range":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockThanos.Close()

	client := NewThanosClient(mockThanos.URL)
	handler := http.StripPrefix("/prometheus", client.PrometheusProxyHandler())

	// 0. /api/v1/label/job/values intercept adds start/end
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/prometheus/api/v1/label/job/values", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var jobLabelsResp promLabelValuesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobLabelsResp))
	assert.Equal(t, []string{"gce-5000Nodes", "gce-100Nodes"}, jobLabelsResp.Data)

	// 1. /api/v1/series Case 1: match[]=perfdash_metric (no job=)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/prometheus/api/v1/series?match[]=perfdash_metric", nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var seriesResp promSeriesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seriesResp))
	assert.Equal(t, []map[string]string{
		{"__name__": "perfdash_metric", "job": "gce-100Nodes"},
		{"__name__": "perfdash_metric", "job": "gce-5000Nodes"},
	}, seriesResp.Data)

	// 2. /api/v1/series Case 2: match[]=perfdash_metric{job="..."} (no category=)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, `/prometheus/api/v1/series?match[]=perfdash_metric%7Bjob%3D%22gce-5000Nodes%22%7D`, nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	seriesResp = promSeriesResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seriesResp))
	assert.Equal(t, []map[string]string{
		{"__name__": "perfdash_metric", "category": "APIServer"},
		{"__name__": "perfdash_metric", "category": "E2E"},
	}, seriesResp.Data)

	// 3. /api/v1/series Case 3: match[]=perfdash_metric{job="...",category="..."} (no metric=)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, `/prometheus/api/v1/series?match[]=perfdash_metric%7Bjob%3D%22gce-5000Nodes%22%2Ccategory%3D%22APIServer%22%7D`, nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	seriesResp = promSeriesResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seriesResp))
	assert.Equal(t, []map[string]string{
		{"__name__": "perfdash_metric", "metric": "LoadResponsiveness_PrometheusSimple"},
	}, seriesResp.Data)

	// 4. /api/v1/series Case 4: match[]=perfdash_metric{job="...",category="...",metric="..."} + cache verification
	for i := 0; i < 5; i++ {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, `/prometheus/api/v1/series?match[]=perfdash_metric%7Bjob%3D%22gce-5000Nodes%22%2Ccategory%3D%22APIServer%22%2Cmetric%3D%22LoadResponsiveness_PrometheusSimple%22%7D`, nil)
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		seriesResp = promSeriesResponse{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seriesResp))
		require.Len(t, seriesResp.Data, 1)
		assert.Equal(t, "perfdash_metric", seriesResp.Data[0]["__name__"])
		assert.Equal(t, "pods", seriesResp.Data[0]["Resource"])
		assert.Equal(t, "WATCHLIST", seriesResp.Data[0]["Verb"])
	}
	mu.Lock()
	assert.Equal(t, 1, counts["/api/v1/query"], "expected 5 identical Case 4 series calls to hit the cache and execute only 1 PromQL query")
	mu.Unlock()

	// 5. Passthrough non-perfdash_metric /api/v1/series and /api/v1/query_range
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, `/prometheus/api/v1/series?match[]=apiserver_request_total%7Brun%3D%22run-123%22%7D`, nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	seriesResp = promSeriesResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seriesResp))
	require.Len(t, seriesResp.Data, 1)
	assert.Equal(t, "apiserver_request_total", seriesResp.Data[0]["__name__"])

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, `/prometheus/api/v1/query_range?query=up`, nil)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
