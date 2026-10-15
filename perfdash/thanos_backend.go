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
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/tsdb"
	"github.com/prometheus/tsdb/labels"
	"k8s.io/klog"
	"k8s.io/kubernetes/test/e2e/perftype"
)

const (
	defaultHeadChunkRangeMS = 2 * 60 * 60 * 1000
	compactionChunkRangeMS  = 10 * 365 * 24 * 60 * 60 * 1000
	compactionBatchSize     = 25
	metricNameLabelValue    = "perfdash_metric"

	defaultLabelQueryStart = "0"
	defaultLabelQueryEnd   = "9999999999"
	// perfdashJobLabelQueryStart (2001-09-09T01:46:40Z) bounds unfiltered "job" label queries
	// to real Prow build timestamps so Thanos Store skips 2000-01-01 snapshot blocks and reads
	// external "job" labels in ~3ms without scanning inner scrape "job" labels.
	perfdashJobLabelQueryStart = "1000000000"
	buildsLookbackWindow       = "10y"
	seriesCacheTTL             = 30 * time.Second
	maxSeriesCacheEntries      = 256
	maxSeriesFormBodyBytes     = 64 * 1024

	// emptyLabelValueSentinel preserves empty label values (such as Subresource="")
	// across Prometheus TSDB, which otherwise drops labels with empty values.
	emptyLabelValueSentinel = "__perfdash_empty__"
	encodedLabelKeyPrefix   = "perfdash_lbl_"
)

// ThanosIngester writes Prow build metrics into native Thanos TSDB blocks.
type ThanosIngester struct {
	tsdbDir string
	logger  log.Logger

	// mu guards ingestedBuilds and jobLocks across concurrent job downloader goroutines.
	mu             sync.RWMutex
	ingestedBuilds map[string]bool
	jobLocks       map[string]*sync.Mutex
}

// NewThanosIngester creates a new ThanosIngester.
func NewThanosIngester(tsdbDir string) *ThanosIngester {
	if abs, err := filepath.Abs(tsdbDir); err == nil {
		tsdbDir = abs
	}
	return &ThanosIngester{
		tsdbDir:        tsdbDir,
		logger:         klogAdapter{},
		ingestedBuilds: make(map[string]bool),
		jobLocks:       make(map[string]*sync.Mutex),
	}
}

// Bootstrap indexes existing TSDB blocks on disk so previously ingested builds are skipped.
func (ti *ThanosIngester) Bootstrap() error {
	files, err := os.ReadDir(ti.tsdbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading tsdb dir %s: %w", ti.tsdbDir, err)
	}

	ti.mu.Lock()
	defer ti.mu.Unlock()

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		blockDir := filepath.Join(ti.tsdbDir, file.Name())
		if strings.HasPrefix(file.Name(), ".staging-") {
			if removeErr := os.RemoveAll(blockDir); removeErr != nil {
				klog.Warningf("Removing orphaned staging directory %s: %v", blockDir, removeErr)
			}
			continue
		}
		if strings.HasPrefix(file.Name(), ".") {
			continue
		}
		meta, err := readBlockMeta(blockDir)
		if err != nil {
			klog.Warningf("Skipping unreadable block metadata in %s: %v", blockDir, err)
			continue
		}
		jobPrefix := meta.Thanos.Labels["job"]
		if jobPrefix == "" {
			continue
		}
		for _, b := range meta.Thanos.Builds {
			if b != "" {
				ti.ingestedBuilds[buildKey(jobPrefix, b)] = true
			}
		}
	}
	return nil
}

// HasBuild reports whether the specified job and build number have already been ingested.
func (ti *ThanosIngester) HasBuild(jobPrefix string, buildNumber int) bool {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return ti.ingestedBuilds[buildKey(jobPrefix, strconv.Itoa(buildNumber))]
}

// IngestBuild writes metric samples for a single build into a native Thanos TSDB block.
func (ti *ThanosIngester) IngestBuild(ctx context.Context, jobPrefix string, buildNumber int, categoryMap CategoryToMetricData, buildTimestamp time.Time) error {
	timestampMS := buildTimestamp.UnixMilli()
	if timestampMS <= 0 {
		return fmt.Errorf("invalid build timestamp %v for %s build %d", buildTimestamp, jobPrefix, buildNumber)
	}

	jmu := ti.jobMutex(jobPrefix)
	jmu.Lock()
	defer jmu.Unlock()

	buildStr := strconv.Itoa(buildNumber)
	head, prowJob, sampleCount, err := ti.buildTSDBHead(jobPrefix, buildStr, categoryMap, timestampMS)
	if err != nil {
		return err
	}
	if head == nil {
		return nil
	}
	defer func() {
		if closeErr := head.Close(); closeErr != nil {
			klog.Warningf("Closing TSDB head for %s build %s: %v", jobPrefix, buildStr, closeErr)
		}
	}()

	blockULID, err := ti.writeTSDBBlock(ctx, head, jobPrefix, prowJob, buildStr)
	if err != nil {
		return err
	}

	ti.markBuildIngested(jobPrefix, buildStr)
	klog.Infof("Created Thanos TSDB block for %s build %s: %s (%d samples)", jobPrefix, buildStr, blockULID, sampleCount)
	return nil
}

// CompactJobBlocks merges all TSDB blocks belonging to jobPrefix into a single TSDB block
// and prunes builds older than the newest maxBuildsToKeep when maxBuildsToKeep > 0.
func (ti *ThanosIngester) CompactJobBlocks(ctx context.Context, jobPrefix string, maxBuildsToKeep int) error {
	jmu := ti.jobMutex(jobPrefix)
	jmu.Lock()
	defer jmu.Unlock()

	blocks, buildSet, prowJob, err := listJobBlocks(ti.tsdbDir, jobPrefix)
	if err != nil {
		return err
	}

	needsTombstoneCleanup := false
	if maxBuildsToKeep > 0 && len(buildSet) > maxBuildsToKeep {
		blocks, needsTombstoneCleanup, err = ti.pruneExpiredBuilds(blocks, buildSet, jobPrefix, maxBuildsToKeep)
		if err != nil {
			return err
		}
	}

	if len(blocks) == 0 || (len(blocks) == 1 && !needsTombstoneCleanup) {
		return nil
	}

	return ti.mergeJobBlocks(ctx, jobPrefix, prowJob, blocks, buildSet, needsTombstoneCleanup)
}

func (ti *ThanosIngester) jobMutex(jobPrefix string) *sync.Mutex {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	m, ok := ti.jobLocks[jobPrefix]
	if !ok {
		m = &sync.Mutex{}
		ti.jobLocks[jobPrefix] = m
	}
	return m
}

func (ti *ThanosIngester) buildTSDBHead(jobPrefix, buildStr string, categoryMap CategoryToMetricData, timestampMS int64) (*tsdb.Head, string, int, error) {
	head, err := tsdb.NewHead(nil, ti.logger, nil, defaultHeadChunkRangeMS)
	if err != nil {
		return nil, "", 0, fmt.Errorf("creating TSDB Head: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			if closeErr := head.Close(); closeErr != nil {
				klog.Warningf("Closing TSDB head on abort: %v", closeErr)
			}
		}
	}()

	if err := head.Init(0); err != nil {
		return nil, "", 0, fmt.Errorf("initializing TSDB Head: %w", err)
	}

	app := head.Appender()
	prowJob := jobPrefix
	sampleCount := 0
	seenSeries := make(map[string]bool)

	for category, metricMap := range categoryMap {
		for metricName, buildData := range metricMap {
			if buildData.Job != "" {
				prowJob = buildData.Job
			}
			for _, item := range buildData.Builds.Builds(buildStr) {
				for streamName, val := range item.Data {
					if math.IsNaN(val) || math.IsInf(val, 0) {
						continue
					}
					lset := encodeSeriesLabels(jobPrefix, prowJob, category, metricName, buildStr, streamName, item)
					seriesKey := lset.String()
					if seenSeries[seriesKey] {
						klog.Warningf("Skipping duplicate series in %s build %s: %s", jobPrefix, buildStr, seriesKey)
						continue
					}
					seenSeries[seriesKey] = true

					if _, err := app.Add(lset, timestampMS, val); err != nil {
						if rbErr := app.Rollback(); rbErr != nil {
							klog.Warningf("Rolling back TSDB appender: %v", rbErr)
						}
						return nil, "", 0, fmt.Errorf("appending sample to TSDB: %w", err)
					}
					sampleCount++
				}
			}
		}
	}

	if sampleCount == 0 {
		if rbErr := app.Rollback(); rbErr != nil {
			klog.Warningf("Rolling back empty TSDB appender: %v", rbErr)
		}
		return nil, "", 0, nil
	}

	if err := app.Commit(); err != nil {
		return nil, "", 0, fmt.Errorf("committing TSDB appender: %w", err)
	}

	succeeded = true
	return head, prowJob, sampleCount, nil
}

func (ti *ThanosIngester) writeTSDBBlock(ctx context.Context, head *tsdb.Head, jobPrefix, prowJob, buildStr string) (string, error) {
	if err := os.MkdirAll(ti.tsdbDir, 0755); err != nil {
		return "", fmt.Errorf("creating tsdb dir %s: %w", ti.tsdbDir, err)
	}

	stagingDir, err := os.MkdirTemp(ti.tsdbDir, ".staging-block-*")
	if err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, ti.logger, []int64{defaultHeadChunkRangeMS}, nil)
	if err != nil {
		return "", fmt.Errorf("creating TSDB compactor: %w", err)
	}

	blockULID, err := compactor.Write(stagingDir, head, head.MinTime(), head.MaxTime()+1, nil)
	if err != nil {
		return "", fmt.Errorf("writing TSDB block: %w", err)
	}

	stagingBlockDir := filepath.Join(stagingDir, blockULID.String())
	err = writeBlockMeta(stagingBlockDir, thanosMeta{
		Labels: map[string]string{
			"job":      jobPrefix,
			"prow_job": prowJob,
		},
		Builds: []string{buildStr},
		Source: "perfdash-ingest",
	})
	if err != nil {
		return "", err
	}

	finalBlockDir := filepath.Join(ti.tsdbDir, blockULID.String())
	if err := os.Rename(stagingBlockDir, finalBlockDir); err != nil {
		return "", fmt.Errorf("moving staged block to %s: %w", finalBlockDir, err)
	}

	return blockULID.String(), nil
}

func (ti *ThanosIngester) markBuildIngested(jobPrefix, buildStr string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	ti.ingestedBuilds[buildKey(jobPrefix, buildStr)] = true
}

func (ti *ThanosIngester) unmarkBuildsIngested(jobPrefix string, expiredBuilds map[string]bool) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	for exp := range expiredBuilds {
		delete(ti.ingestedBuilds, buildKey(jobPrefix, exp))
	}
}

func buildKey(jobPrefix, buildStr string) string {
	return fmt.Sprintf("%s/%s", jobPrefix, buildStr)
}

func listJobBlocks(tsdbDir, jobPrefix string) ([]jobBlockInfo, map[string]bool, string, error) {
	files, err := os.ReadDir(tsdbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, jobPrefix, nil
		}
		return nil, nil, "", err
	}

	var blocks []jobBlockInfo
	buildSet := make(map[string]bool)
	prowJob := jobPrefix

	for _, file := range files {
		if !file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}
		blockDir := filepath.Join(tsdbDir, file.Name())
		meta, err := readBlockMeta(blockDir)
		if err != nil {
			klog.Warningf("Skipping unreadable block metadata in %s: %v", blockDir, err)
			continue
		}
		if meta.Thanos.Labels["job"] != jobPrefix {
			continue
		}
		if pj := meta.Thanos.Labels["prow_job"]; pj != "" {
			prowJob = pj
		}
		var blockBuilds []string
		for _, b := range meta.Thanos.Builds {
			if b != "" {
				buildSet[b] = true
				blockBuilds = append(blockBuilds, b)
			}
		}
		blocks = append(blocks, jobBlockInfo{
			dir:     blockDir,
			minTime: meta.MinTime,
			builds:  blockBuilds,
			thanos:  meta.Thanos,
		})
	}
	return blocks, buildSet, prowJob, nil
}

func (ti *ThanosIngester) pruneExpiredBuilds(blocks []jobBlockInfo, buildSet map[string]bool, jobPrefix string, maxBuildsToKeep int) ([]jobBlockInfo, bool, error) {
	expiredSet := findExpiredBuilds(buildSet, maxBuildsToKeep)
	var keptBlocks []jobBlockInfo
	needsTombstoneCleanup := false

	for _, b := range blocks {
		var expiredInBlock []string
		var remainingInBlock []string
		for _, buildStr := range b.builds {
			if expiredSet[buildStr] {
				expiredInBlock = append(expiredInBlock, buildStr)
			} else {
				remainingInBlock = append(remainingInBlock, buildStr)
			}
		}
		if len(expiredInBlock) > 0 && len(expiredInBlock) == len(b.builds) {
			if err := os.RemoveAll(b.dir); err != nil {
				return nil, false, fmt.Errorf("removing expired block %s: %w", b.dir, err)
			}
			continue
		}
		if len(expiredInBlock) > 0 {
			updatedThanos := b.thanos
			updatedThanos.Builds = remainingInBlock
			if err := ti.deleteBuildsFromBlock(b.dir, expiredInBlock, updatedThanos); err != nil {
				return nil, false, err
			}
			b.builds = remainingInBlock
			b.thanos = updatedThanos
			needsTombstoneCleanup = true
		}
		keptBlocks = append(keptBlocks, b)
	}

	for exp := range expiredSet {
		delete(buildSet, exp)
	}
	ti.unmarkBuildsIngested(jobPrefix, expiredSet)
	return keptBlocks, needsTombstoneCleanup, nil
}

func (ti *ThanosIngester) deleteBuildsFromBlock(blockDir string, expiredBuilds []string, updatedThanos thanosMeta) error {
	pb, err := tsdb.OpenBlock(ti.logger, blockDir, nil)
	if err != nil {
		return fmt.Errorf("opening block %s to prune expired builds: %w", blockDir, err)
	}
	for _, exp := range expiredBuilds {
		if err := pb.Delete(pb.MinTime(), pb.MaxTime(), labels.NewEqualMatcher("build", exp)); err != nil {
			_ = pb.Close()
			return fmt.Errorf("deleting expired build %s from %s: %w", exp, blockDir, err)
		}
	}
	if err := pb.Close(); err != nil {
		return fmt.Errorf("closing block %s after pruning builds: %w", blockDir, err)
	}
	// tsdb.Block.Delete rewrites meta.json from tsdb.BlockMeta without the thanos extension,
	// so re-apply the updated thanos metadata before returning.
	return writeBlockMeta(blockDir, updatedThanos)
}

func findExpiredBuilds(buildSet map[string]bool, maxBuildsToKeep int) map[string]bool {
	if maxBuildsToKeep <= 0 || len(buildSet) <= maxBuildsToKeep {
		return nil
	}
	all := make([]string, 0, len(buildSet))
	for b := range buildSet {
		all = append(all, b)
	}
	sort.Slice(all, func(i, j int) bool {
		ni, errI := strconv.Atoi(all[i])
		nj, errJ := strconv.Atoi(all[j])
		if errI == nil && errJ == nil {
			return ni > nj
		}
		return all[i] > all[j]
	})
	expired := make(map[string]bool)
	for _, b := range all[maxBuildsToKeep:] {
		expired[b] = true
	}
	return expired
}

func (ti *ThanosIngester) mergeJobBlocks(ctx context.Context, jobPrefix, prowJob string, blocks []jobBlockInfo, buildSet map[string]bool, needsTombstoneCleanup bool) error {
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].minTime < blocks[j].minTime
	})

	stagingDir, err := os.MkdirTemp(ti.tsdbDir, ".staging-compact-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingDir)

	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, ti.logger, []int64{compactionChunkRangeMS}, nil)
	if err != nil {
		return err
	}

	currentDirs := make([]string, len(blocks))
	for i, b := range blocks {
		currentDirs[i] = b.dir
	}

	if len(currentDirs) == 1 && needsTombstoneCleanup {
		uid, err := compactor.Compact(stagingDir, currentDirs, nil)
		if err != nil {
			return fmt.Errorf("cleaning tombstones for %s: %w", jobPrefix, err)
		}
		currentDirs = []string{filepath.Join(stagingDir, uid.String())}
	}

	for len(currentDirs) > 1 {
		var nextDirs []string
		for i := 0; i < len(currentDirs); i += compactionBatchSize {
			end := i + compactionBatchSize
			if end > len(currentDirs) {
				end = len(currentDirs)
			}
			batch := currentDirs[i:end]
			if len(batch) == 1 {
				nextDirs = append(nextDirs, batch[0])
				continue
			}
			uid, err := compactor.Compact(stagingDir, batch, nil)
			if err != nil {
				return fmt.Errorf("compacting batch for %s: %w", jobPrefix, err)
			}
			nextDirs = append(nextDirs, filepath.Join(stagingDir, uid.String()))
		}
		currentDirs = nextDirs
	}

	compactedBlockDir := currentDirs[0]
	finalULID := filepath.Base(compactedBlockDir)

	allBuilds := make([]string, 0, len(buildSet))
	for b := range buildSet {
		allBuilds = append(allBuilds, b)
	}
	sort.Strings(allBuilds)

	err = writeBlockMeta(compactedBlockDir, thanosMeta{
		Labels: map[string]string{
			"job":      jobPrefix,
			"prow_job": prowJob,
		},
		Builds: allBuilds,
		Source: "perfdash-ingest",
	})
	if err != nil {
		return err
	}

	finalBlockDir := filepath.Join(ti.tsdbDir, finalULID)
	if err := os.Rename(compactedBlockDir, finalBlockDir); err != nil {
		return err
	}

	for _, orig := range blocks {
		if err := os.RemoveAll(orig.dir); err != nil {
			klog.Warningf("Removing compacted source block %s: %v", orig.dir, err)
		}
	}

	klog.Infof("Compacted %d blocks for job %s into %s (%d builds)", len(blocks), jobPrefix, finalULID, len(allBuilds))
	return nil
}

func readBlockMeta(blockDir string) (*blockMeta, error) {
	metaBytes, err := os.ReadFile(filepath.Join(blockDir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var meta blockMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}
	if meta.Thanos.Labels == nil {
		return nil, fmt.Errorf("missing thanos.labels in %s/meta.json", blockDir)
	}
	return &meta, nil
}

func writeBlockMeta(blockDir string, meta thanosMeta) error {
	metaPath := filepath.Join(blockDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("reading meta.json: %w", err)
	}
	var metaObj map[string]json.RawMessage
	if err := json.Unmarshal(metaBytes, &metaObj); err != nil {
		return fmt.Errorf("unmarshaling meta.json: %w", err)
	}
	thanosBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling thanos metadata: %w", err)
	}
	metaObj["thanos"] = thanosBytes
	updatedBytes, err := json.MarshalIndent(metaObj, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling meta.json: %w", err)
	}
	if err := os.WriteFile(metaPath, updatedBytes, 0644); err != nil {
		return fmt.Errorf("writing meta.json: %w", err)
	}
	return nil
}

// ThanosClient queries Thanos Query API for perfdash metrics.
type ThanosClient struct {
	queryURL       string
	client         *http.Client
	seriesMu       sync.Mutex
	seriesCache    map[string]seriesCacheEntry
	seriesInflight map[string]*inflightSeriesCall
}

// NewThanosClient creates a new ThanosClient.
func NewThanosClient(queryURL string) *ThanosClient {
	return &ThanosClient{
		queryURL:       strings.TrimRight(queryURL, "/"),
		client:         &http.Client{Timeout: 30 * time.Second},
		seriesCache:    make(map[string]seriesCacheEntry),
		seriesInflight: make(map[string]*inflightSeriesCall),
	}
}

// PrometheusProxyHandler returns an http.Handler that proxies Prometheus HTTP API
// requests to Thanos Query while optimizing high-cardinality perfdash_metric
// metadata queries (/api/v1/series and /api/v1/label/job/values) for Grafana 6.2.0.
func (tc *ThanosClient) PrometheusProxyHandler() http.Handler {
	targetURL, err := url.Parse(tc.queryURL)
	var proxy *httputil.ReverseProxy
	if err == nil {
		proxy = httputil.NewSingleHostReverseProxy(targetURL)
		origDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			req.Host = targetURL.Host
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && (r.URL.Path == "/api/v1/labels" || (strings.HasPrefix(r.URL.Path, "/api/v1/label/") && strings.HasSuffix(r.URL.Path, "/values"))) {
			q := r.URL.Query()
			if q.Get("start") == "" {
				r = r.Clone(r.Context())
				start := defaultLabelQueryStart
				if r.URL.Path == "/api/v1/label/job/values" && q.Get("match[]") == "" {
					start = perfdashJobLabelQueryStart
				}
				q.Set("start", start)
				q.Set("end", defaultLabelQueryEnd)
				r.URL.RawQuery = q.Encode()
			}
		}

		if r.URL.Path == "/api/v1/series" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
			if matchExpr, ok := extractSeriesMatchParam(r); ok {
				if matchers, ok := parsePerfdashSeriesSelector(matchExpr); ok {
					body, err := tc.getOrComputeSeries(r.Context(), matchExpr, matchers)
					if err != nil {
						klog.Errorf("PrometheusProxyHandler series query (%s) failed: %v", matchExpr, err)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadGateway)
						_ = json.NewEncoder(w).Encode(promSeriesResponse{
							Status: "error",
							Error:  err.Error(),
						})
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
					return
				}
			}
		}

		if proxy == nil {
			http.Error(w, "invalid thanos query URL", http.StatusInternalServerError)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// ServeJobNames serves /jobnames from Thanos.
func (tc *ThanosClient) ServeJobNames(res http.ResponseWriter, req *http.Request) {
	tc.serveDistinctLabel(res, req, fmt.Sprintf(`{__name__=%q}`, metricNameLabelValue), "job")
}

// ServeCategoryNames serves /metriccategorynames from Thanos.
func (tc *ThanosClient) ServeCategoryNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		http.Error(res, "missing required parameter: jobname", http.StatusBadRequest)
		return
	}
	tc.serveDistinctLabel(res, req, fmt.Sprintf(`{__name__=%q,job=%q}`, metricNameLabelValue, jobname), "category")
}

// ServeMetricNames serves /metricnames from Thanos.
func (tc *ThanosClient) ServeMetricNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		http.Error(res, "missing required parameter: jobname", http.StatusBadRequest)
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		http.Error(res, "missing required parameter: metriccategoryname", http.StatusBadRequest)
		return
	}
	tc.serveDistinctLabel(res, req, fmt.Sprintf(`{__name__=%q,job=%q,category=%q}`, metricNameLabelValue, jobname, categoryname), "metric")
}

// ServeBuildsData serves /buildsdata reconstructed from Thanos PromQL queries.
func (tc *ThanosClient) ServeBuildsData(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		http.Error(res, "missing required parameter: jobname", http.StatusBadRequest)
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		http.Error(res, "missing required parameter: metriccategoryname", http.StatusBadRequest)
		return
	}
	metricname, ok := getURLParam(req, "metricname")
	if !ok || metricname == "" {
		http.Error(res, "missing required parameter: metricname", http.StatusBadRequest)
		return
	}

	query := fmt.Sprintf(`last_over_time(%s{job=%q,category=%q,metric=%q}[%s])`, metricNameLabelValue, jobname, categoryname, metricname, buildsLookbackWindow)
	queryResp, err := tc.QueryPromQL(req.Context(), query)
	if err != nil {
		klog.Errorf("Thanos ServeBuildsData query failed: %v", err)
		http.Error(res, fmt.Sprintf("querying builds data: %v", err), http.StatusInternalServerError)
		return
	}

	// PerfDash keys API queries by tests.Prefix (jobname), whereas BuildData.Job holds the
	// underlying Prow job name (stored in the prow_job label) so clicking a point in the UI
	// opens the corresponding Prow GCS build log directory.
	prowJobName := jobname
	foundProwJob := false
	buildsMap := make(map[string]map[string]*perftype.DataItem)
	for _, result := range queryResp.Data.Result {
		build := result.Metric["build"]
		stream := result.Metric["percentile"]
		if build == "" || stream == "" {
			continue
		}
		if pj := result.Metric["prow_job"]; pj != "" && !foundProwJob {
			prowJobName = pj
			foundProwJob = true
		}

		floatVal, err := parsePromSampleValue(result.Value)
		if err != nil {
			klog.Errorf("Skipping invalid sample value for job %s build %s metric %s: %v", jobname, build, metricname, err)
			continue
		}

		otherLabels := decodeSeriesLabels(result.Metric)
		groupKey := createMapID(otherLabels)

		if _, ok := buildsMap[build]; !ok {
			buildsMap[build] = make(map[string]*perftype.DataItem)
		}
		dataItem, exists := buildsMap[build][groupKey]
		if !exists {
			var itemLabels map[string]string
			if len(otherLabels) > 0 {
				itemLabels = otherLabels
			}
			dataItem = &perftype.DataItem{
				Data:   make(map[string]float64),
				Unit:   result.Metric["unit"],
				Labels: itemLabels,
			}
			buildsMap[build][groupKey] = dataItem
		}
		dataItem.Data[stream] = floatVal
	}

	resultBuilds := make(map[string][]perftype.DataItem, len(buildsMap))
	for build, groupMap := range buildsMap {
		groupKeys := make([]string, 0, len(groupMap))
		for k := range groupMap {
			groupKeys = append(groupKeys, k)
		}
		sort.Strings(groupKeys)
		items := make([]perftype.DataItem, 0, len(groupKeys))
		for _, k := range groupKeys {
			items = append(items, *groupMap[k])
		}
		resultBuilds[build] = items
	}

	serveHTTPObject(res, req, &BuildData{
		Job:     prowJobName,
		Version: "v1",
		Builds:  NewBuilds(resultBuilds),
	})
}

// QueryLabelValues queries /api/v1/label/<labelName>/values with an optional match[] selector.
func (tc *ThanosClient) QueryLabelValues(ctx context.Context, matchExpr, labelKey string) ([]string, error) {
	start := defaultLabelQueryStart
	if labelKey == "job" && matchExpr == "" {
		start = perfdashJobLabelQueryStart
	}
	u := fmt.Sprintf("%s/api/v1/label/%s/values?start=%s&end=%s",
		tc.queryURL, url.PathEscape(labelKey), start, defaultLabelQueryEnd)
	if matchExpr != "" {
		u += "&match[]=" + url.QueryEscape(matchExpr)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating label values request: %w", err)
	}
	resp, err := tc.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("querying thanos label values: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading label values response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thanos label values returned status %d: %s", resp.StatusCode, string(body))
	}
	var labelResp promLabelValuesResponse
	if err := json.Unmarshal(body, &labelResp); err != nil {
		return nil, fmt.Errorf("unmarshaling label values response: %w", err)
	}
	if labelResp.Status != "success" {
		return nil, fmt.Errorf("thanos label values error: %s", labelResp.Error)
	}
	sort.Strings(labelResp.Data)
	return labelResp.Data, nil
}

// QueryPromQL executes a PromQL query against Thanos Query.
func (tc *ThanosClient) QueryPromQL(ctx context.Context, queryStr string) (*promQueryResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", tc.queryURL, url.QueryEscape(queryStr))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating promql request: %w", err)
	}
	resp, err := tc.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("querying thanos promql: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading promql response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thanos query returned status %d: %s", resp.StatusCode, string(body))
	}

	var queryResp promQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("unmarshaling promql response: %w", err)
	}
	if queryResp.Status != "success" {
		return nil, fmt.Errorf("thanos query error: %s", queryResp.Error)
	}
	return &queryResp, nil
}

func (tc *ThanosClient) serveDistinctLabel(res http.ResponseWriter, req *http.Request, matchExpr, labelKey string) {
	out, err := tc.QueryLabelValues(req.Context(), matchExpr, labelKey)
	if err != nil {
		klog.Errorf("Thanos label values query (%s) failed: %v", matchExpr, err)
		http.Error(res, fmt.Sprintf("querying label %s: %v", labelKey, err), http.StatusInternalServerError)
		return
	}
	serveHTTPObject(res, req, &out)
}

func (tc *ThanosClient) getOrComputeSeries(ctx context.Context, matchExpr string, matchers map[string]string) ([]byte, error) {
	tc.seriesMu.Lock()
	now := time.Now()
	if entry, ok := tc.seriesCache[matchExpr]; ok && now.Before(entry.expiresAt) {
		tc.seriesMu.Unlock()
		return entry.data, nil
	}
	if call, ok := tc.seriesInflight[matchExpr]; ok {
		tc.seriesMu.Unlock()
		select {
		case <-call.done:
			return call.data, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &inflightSeriesCall{done: make(chan struct{})}
	tc.seriesInflight[matchExpr] = call
	tc.seriesMu.Unlock()

	// Run upstream series computation on an independent timeout context so that a single
	// canceled HTTP request does not abort the shared result for other concurrent waiters.
	go func() {
		computeCtx, cancel := context.WithTimeout(context.Background(), tc.client.Timeout)
		defer cancel()
		body, err := tc.computeSeries(computeCtx, matchers)

		tc.seriesMu.Lock()
		if err == nil {
			now := time.Now()
			tc.evictExpiredSeriesCacheLocked(now)
			tc.seriesCache[matchExpr] = seriesCacheEntry{
				data:      body,
				expiresAt: now.Add(seriesCacheTTL),
			}
		}
		delete(tc.seriesInflight, matchExpr)
		call.data = body
		call.err = err
		close(call.done)
		tc.seriesMu.Unlock()
	}()

	select {
	case <-call.done:
		return call.data, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// computeSeries resolves Grafana 6.2.0's /api/v1/series?match[]=perfdash_metric{...}
// variable discovery queries without scanning full TSDB postings across all blocks:
//   - Level 1 (no job filter): reads Thanos external block label "job" via /api/v1/label/job/values.
//   - Level 2 (job only): reads "category" values for that job block via /api/v1/label/category/values.
//   - Level 3 (job + category): reads "metric" values via /api/v1/label/metric/values.
//   - Level 4 (job + category + metric): deduplicates dynamic item labels across builds via PromQL.
func (tc *ThanosClient) computeSeries(ctx context.Context, matchers map[string]string) ([]byte, error) {
	seriesData := make([]map[string]string, 0)
	job, hasJob := matchers["job"]
	category, hasCategory := matchers["category"]
	metric, hasMetric := matchers["metric"]

	switch {
	case !hasJob:
		vals, err := tc.QueryLabelValues(ctx, "", "job")
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			seriesData = append(seriesData, map[string]string{
				"__name__": metricNameLabelValue,
				"job":      v,
			})
		}
	case !hasCategory:
		selector := fmt.Sprintf(`%s{job=%q}`, metricNameLabelValue, job)
		vals, err := tc.QueryLabelValues(ctx, selector, "category")
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			seriesData = append(seriesData, map[string]string{
				"__name__": metricNameLabelValue,
				"category": v,
			})
		}
	case !hasMetric:
		selector := fmt.Sprintf(`%s{job=%q,category=%q}`, metricNameLabelValue, job, category)
		vals, err := tc.QueryLabelValues(ctx, selector, "metric")
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			seriesData = append(seriesData, map[string]string{
				"__name__": metricNameLabelValue,
				"metric":   v,
			})
		}
	default:
		promQL := fmt.Sprintf(`count without (build, prow_job) (last_over_time(%s{job=%q,category=%q,metric=%q}[%s]))`,
			metricNameLabelValue, job, category, metric, buildsLookbackWindow)
		queryResp, err := tc.QueryPromQL(ctx, promQL)
		if err != nil {
			return nil, err
		}
		for _, res := range queryResp.Data.Result {
			m := make(map[string]string, len(res.Metric)+1)
			for k, v := range res.Metric {
				if v == emptyLabelValueSentinel {
					continue
				}
				m[k] = v
			}
			m["__name__"] = metricNameLabelValue
			seriesData = append(seriesData, m)
		}
	}
	return json.Marshal(promSeriesResponse{
		Status: "success",
		Data:   seriesData,
	})
}

func (tc *ThanosClient) evictExpiredSeriesCacheLocked(now time.Time) {
	for k, v := range tc.seriesCache {
		if !now.Before(v.expiresAt) {
			delete(tc.seriesCache, k)
		}
	}
	if len(tc.seriesCache) >= maxSeriesCacheEntries {
		tc.seriesCache = make(map[string]seriesCacheEntry)
	}
}

func extractSeriesMatchParam(r *http.Request) (string, bool) {
	if matches := r.URL.Query()["match[]"]; len(matches) > 0 {
		return matches[0], len(matches) == 1
	}
	if r.Method == http.MethodPost && r.Body != nil {
		bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxSeriesFormBodyBytes+1))
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), r.Body))
		if err == nil && len(bodyBytes) <= maxSeriesFormBodyBytes {
			if vals, err := url.ParseQuery(string(bodyBytes)); err == nil {
				if matches := vals["match[]"]; len(matches) > 0 {
					return matches[0], len(matches) == 1
				}
			}
		}
	}
	return "", false
}

var (
	perfdashSelectorRe = regexp.MustCompile(`^\s*perfdash_metric\s*(?:\{\s*([^{}]*?)\s*\})?\s*$`)
	labelMatcherRe     = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*"((?:[^"\\]|\\.)*)"$`)
)

func parsePerfdashSeriesSelector(matchExpr string) (map[string]string, bool) {
	m := perfdashSelectorRe.FindStringSubmatch(matchExpr)
	if m == nil {
		return nil, false
	}
	matchers := make(map[string]string)
	body := strings.TrimSpace(m[1])
	if body == "" {
		return matchers, true
	}
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := labelMatcherRe.FindStringSubmatch(part)
		if kv == nil {
			return nil, false
		}
		key := kv[1]
		if key != "job" && key != "category" && key != "metric" {
			return nil, false
		}
		if _, dup := matchers[key]; dup {
			return nil, false
		}
		unquoted, err := strconv.Unquote(`"` + kv[2] + `"`)
		if err != nil {
			return nil, false
		}
		matchers[key] = unquoted
	}
	_, hasJob := matchers["job"]
	_, hasCategory := matchers["category"]
	_, hasMetric := matchers["metric"]
	if (hasCategory && !hasJob) || (hasMetric && (!hasJob || !hasCategory)) {
		return nil, false
	}
	return matchers, true
}

// parsePromSampleValue extracts the float64 metric value from a Prometheus
// instant vector sample tuple: [ <unix_timestamp_float>, "<sample_value_string>" ].
func parsePromSampleValue(sampleTuple []interface{}) (float64, error) {
	if len(sampleTuple) != 2 {
		return 0, fmt.Errorf("expected [timestamp, value] tuple of length 2, got %d elements", len(sampleTuple))
	}
	strVal, ok := sampleTuple[1].(string)
	if !ok {
		return 0, fmt.Errorf("expected string sample value at index 1, got %T", sampleTuple[1])
	}
	return strconv.ParseFloat(strVal, 64)
}

var reservedTSDBLabels = map[string]bool{
	"__name__":   true,
	"job":        true,
	"prow_job":   true,
	"category":   true,
	"metric":     true,
	"build":      true,
	"percentile": true,
	"unit":       true,
}

func encodeSeriesLabels(jobPrefix, prowJob, category, metricName, buildStr, streamName string, item perftype.DataItem) labels.Labels {
	lblsMap := map[string]string{
		"__name__":   metricNameLabelValue,
		"job":        jobPrefix,
		"prow_job":   prowJob,
		"category":   category,
		"metric":     metricName,
		"build":      buildStr,
		"percentile": streamName,
	}
	if item.Unit != "" {
		lblsMap["unit"] = item.Unit
	}
	for k, v := range item.Labels {
		if k == "" {
			continue
		}
		storeKey := k
		if reservedTSDBLabels[k] || strings.HasPrefix(k, encodedLabelKeyPrefix) || !isValidPromLabelName(k) {
			storeKey = encodedLabelKeyPrefix + hex.EncodeToString([]byte(k))
		}
		if v == "" {
			v = emptyLabelValueSentinel
		}
		lblsMap[storeKey] = v
	}
	return labels.FromMap(lblsMap)
}

func decodeSeriesLabels(metric map[string]string) map[string]string {
	otherLabels := make(map[string]string)
	for k, v := range metric {
		if reservedTSDBLabels[k] {
			continue
		}
		if strings.HasPrefix(k, encodedLabelKeyPrefix) {
			if decoded, err := hex.DecodeString(strings.TrimPrefix(k, encodedLabelKeyPrefix)); err == nil {
				k = string(decoded)
			}
		}
		if v == emptyLabelValueSentinel {
			v = ""
		}
		otherLabels[k] = v
	}
	return otherLabels
}

func isValidPromLabelName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

type klogAdapter struct{}

func (klogAdapter) Log(keyvals ...interface{}) error {
	klog.Infoln(keyvals...)
	return nil
}

type promLabelValuesResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
	Error  string   `json:"error,omitempty"`
}

type promSeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
	Error  string              `json:"error,omitempty"`
}

type seriesCacheEntry struct {
	data      []byte
	expiresAt time.Time
}

type inflightSeriesCall struct {
	done chan struct{}
	data []byte
	err  error
}

type promQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

type blockMeta struct {
	MinTime int64      `json:"minTime"`
	Thanos  thanosMeta `json:"thanos"`
}

type thanosMeta struct {
	Labels map[string]string `json:"labels"`
	Builds []string          `json:"builds,omitempty"`
	Source string            `json:"source,omitempty"`
}

type jobBlockInfo struct {
	dir     string
	minTime int64
	builds  []string
	thanos  thanosMeta
}
