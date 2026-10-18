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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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
)

func createTestSnapshotTarGz(t *testing.T, baseMS int64) []byte {
	t.Helper()
	srcDir := t.TempDir()

	logger := log.NewNopLogger()
	head, err := tsdb.NewHead(nil, logger, nil, 2*60*60*1000)
	require.NoError(t, err)
	require.NoError(t, head.Init(0))

	app := head.Appender()
	lset := labels.FromStrings("__name__", "up", "job", "apiserver", "instance", "10.0.0.1:6443")
	ref, err := app.Add(lset, baseMS, 1.0)
	require.NoError(t, err)
	require.NoError(t, app.AddFast(ref, baseMS+30000, 1.0))
	require.NoError(t, app.AddFast(ref, baseMS+60000, 0.0))
	require.NoError(t, app.Commit())

	compactor, err := tsdb.NewLeveledCompactor(context.Background(), nil, logger, []int64{2 * 60 * 60 * 1000}, nil)
	require.NoError(t, err)
	_, err = compactor.Write(srcDir, head, baseMS, baseMS+60001, nil)
	require.NoError(t, err)
	_ = head.Close()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || path == srcDir {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join("prometheus_snapshot", rel))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, err = tw.Write(data)
			return err
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestSnapshotIngestionAndPruning(t *testing.T) {
	// Original run starts at 2026-10-08T10:00:00Z; CL2 first step is at 10:00:10Z (+10s).
	runStart := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	baseMS := runStart.UnixMilli()
	snapshotTarGz := createTestSnapshotTarGz(t, baseMS)

	job := "ci-kubernetes-e2e-gci-gce-scalability-release-1-34"
	artifactPerf := "artifacts/PodStartupLatency_PodStartupLatency_2026-10-08T10:00:00Z.json"
	snapshotPath := "artifacts/prometheus_snapshot.tar.gz"
	buildLogPath := "build-log.txt"
	startedPath := "started.json"

	buildLogContent := []byte("I1008 09:59:50.000000 1 cluster.go:100] Starting cluster\n" +
		"I1008 10:00:10.123456 1 simple_test_executor.go:163] Step \"[step: 01] Starting measurements\" started\n")

	bucket := &fakeMetricsBucket{
		builds: []int{101},
		files: map[string][]byte{
			joinStringsAndInts(job, 101, artifactPerf): []byte(`{
				"version": "v1",
				"dataItems": [{
					"data": {"Perc50": 100, "Perc90": 200, "Perc99": 300},
					"unit": "ms",
					"labels": {"Metric": "pod_startup"}
				}]
			}`),
			joinStringsAndInts(job, 101, snapshotPath): snapshotTarGz,
			joinStringsAndInts(job, 101, buildLogPath): buildLogContent,
			joinStringsAndInts(job, 101, startedPath):  []byte(`{"timestamp": 1791453600}`),
		},
	}

	tempTSDB := t.TempDir()

	ingester := NewThanosIngester(tempTSDB)
	require.NoError(t, ingester.Bootstrap())

	downloader := NewDownloader(&DownloaderOptions{
		DefaultBuildsCount:  2,
		SnapshotJobs:        []string{"gce-5000Nodes"},
		SnapshotBuildsCount: 2,
	}, bucket, false, ingester)

	result := make(JobToCategoryData)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(1)
	downloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "gce-5000Nodes",
		Descriptions: performanceDescriptions,
		BuildsCount:  2,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	assert.True(t, ingester.HasSnapshot("gce-5000Nodes", 101), "expected snapshot for build 101 to be ingested")

	// Restart ingester and verify Bootstrap discovers the snapshot block.
	restarted := NewThanosIngester(tempTSDB)
	require.NoError(t, restarted.Bootstrap())
	assert.True(t, restarted.HasSnapshot("gce-5000Nodes", 101))

	// Inspect the snapshot block on disk and verify shifted timestamps and external labels.
	entries, err := os.ReadDir(tempTSDB)
	require.NoError(t, err)

	var snapBlockDir string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(tempTSDB, entry.Name())
		meta, err := readBlockMeta(dir)
		require.NoError(t, err)
		if meta.Thanos.Source == snapshotBlockSource {
			snapBlockDir = dir
			assert.Equal(t, "gce-5000Nodes", meta.Thanos.Labels["perflens_job"])
			assert.Equal(t, "run-101", meta.Thanos.Labels["run"])
			_, hasJobLabel := meta.Thanos.Labels["job"]
			assert.False(t, hasJobLabel, "snapshot block must not override inner Prometheus job label")
		}
	}
	require.NotEmpty(t, snapBlockDir, "expected a snapshot TSDB block on disk")

	pb, err := tsdb.OpenBlock(log.NewNopLogger(), snapBlockDir, nil)
	require.NoError(t, err)
	q, err := tsdb.NewBlockQuerier(pb, pb.MinTime(), pb.MaxTime())
	require.NoError(t, err)
	ss, err := q.Select(labels.NewEqualMatcher("__name__", "up"), labels.NewEqualMatcher("job", "apiserver"))
	require.NoError(t, err)
	require.True(t, ss.Next())

	type sample struct {
		t int64
		v float64
	}
	var got []sample
	it := ss.At().Iterator()
	for it.Next() {
		ts, val := it.At()
		got = append(got, sample{t: ts, v: val})
	}
	require.NoError(t, it.Err())
	_ = q.Close()
	_ = pb.Close()

	// CL2 first step was at 10:00:10Z (+10,000ms after baseMS), so baseMS shifts to snapshotAnchorTimeMillis - 10,000ms.
	expectedT0 := snapshotAnchorTimeMillis - 10000
	require.Equal(t, []sample{
		{t: expectedT0, v: 1.0},
		{t: expectedT0 + 30000, v: 1.0},
		{t: expectedT0 + 60000, v: 0.0},
	}, got)

	// Add builds 102 and 103 with SnapshotBuildsCount=2 and verify build 101 snapshot is pruned.
	bucket.builds = []int{101, 102, 103}
	for _, b := range []int{102, 103} {
		bucket.files[joinStringsAndInts(job, b, artifactPerf)] = bucket.files[joinStringsAndInts(job, 101, artifactPerf)]
		bucket.files[joinStringsAndInts(job, b, snapshotPath)] = snapshotTarGz
		bucket.files[joinStringsAndInts(job, b, buildLogPath)] = buildLogContent
		bucket.files[joinStringsAndInts(job, b, startedPath)] = []byte(`{"timestamp": 1791457200}`)
	}

	wg.Add(1)
	downloader.getJobData(&wg, result, &mu, job, Tests{
		Prefix:       "gce-5000Nodes",
		Descriptions: performanceDescriptions,
		BuildsCount:  2,
		ArtifactsDir: "artifacts",
	})
	wg.Wait()

	assert.False(t, ingester.HasSnapshot("gce-5000Nodes", 101), "expired snapshot for build 101 should be pruned")
	assert.True(t, ingester.HasSnapshot("gce-5000Nodes", 102))
	assert.True(t, ingester.HasSnapshot("gce-5000Nodes", 103))

	// Verify extractTarArchive rejects path-traversal and oversized entries.
	var badTar bytes.Buffer
	tw := tar.NewWriter(&badTar)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "../escape.txt",
		Typeflag: tar.TypeReg,
		Size:     4,
		Mode:     0644,
	}))
	_, err = tw.Write([]byte("test"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	assert.Error(t, extractTarArchive(&badTar, t.TempDir()))
}
