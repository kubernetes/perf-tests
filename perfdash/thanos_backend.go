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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// ThanosIngester writes Prow build metrics into native Thanos TSDB blocks.
type ThanosIngester struct {
	tsdbDir        string
	mu             sync.Mutex
	ingestedBuilds map[string]bool
}

// NewThanosIngester creates a new ThanosIngester.
func NewThanosIngester(tsdbDir string) *ThanosIngester {
	return &ThanosIngester{
		tsdbDir:        tsdbDir,
		ingestedBuilds: make(map[string]bool),
	}
}

// Bootstrap indexes existing TSDB blocks on disk and compacts multi-block jobs.
func (ti *ThanosIngester) Bootstrap(ctx context.Context) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	jobCounts := ti.loadExistingBlocksLocked()
	for job, count := range jobCounts {
		if count > 1 {
			if err := ti.compactJobBlocksLocked(ctx, job, 0); err != nil {
				klog.Errorf("Compacting blocks for job %s failed: %v", job, err)
			}
		}
	}
}

// HasBuild reports whether the specified job and build number have already been ingested.
func (ti *ThanosIngester) HasBuild(job string, buildNumber int) bool {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	return ti.ingestedBuilds[fmt.Sprintf("%s/%d", job, buildNumber)]
}

// IngestBuild writes metric samples for a single build into a native Thanos TSDB block.
func (ti *ThanosIngester) IngestBuild(ctx context.Context, job string, buildNumber int, categoryMap CategoryToMetricData, buildTimestamp time.Time) error {
	if err := os.MkdirAll(ti.tsdbDir, 0755); err != nil {
		return fmt.Errorf("creating tsdb dir %s: %w", ti.tsdbDir, err)
	}

	tsdbDirAbs, err := filepath.Abs(ti.tsdbDir)
	if err != nil {
		return fmt.Errorf("resolving abs path for %s: %w", ti.tsdbDir, err)
	}

	logger := log.NewNopLogger()
	head, err := tsdb.NewHead(nil, logger, nil, 2*60*60*1000)
	if err != nil {
		return fmt.Errorf("creating TSDB Head: %w", err)
	}
	defer head.Close()
	if err := head.Init(0); err != nil {
		return fmt.Errorf("initializing TSDB Head: %w", err)
	}

	app := head.Appender()
	timestampMS := buildTimestamp.UnixMilli()
	if timestampMS <= 0 {
		timestampMS = time.Now().UTC().UnixMilli()
	}

	buildStr := strconv.Itoa(buildNumber)
	prowJob := job
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
					lset := encodeSeriesLabels(job, prowJob, category, metricName, buildStr, streamName, item)
					seriesKey := lset.String()
					if seenSeries[seriesKey] {
						continue
					}
					seenSeries[seriesKey] = true

					if _, err := app.Add(lset, timestampMS, val); err != nil {
						return fmt.Errorf("appending sample to TSDB: %w", err)
					}
					sampleCount++
				}
			}
		}
	}

	if sampleCount == 0 {
		return nil
	}

	if err := app.Commit(); err != nil {
		return fmt.Errorf("committing TSDB appender: %w", err)
	}

	ti.mu.Lock()
	defer ti.mu.Unlock()

	stagingDir, err := os.MkdirTemp(tsdbDirAbs, ".staging-block-*")
	if err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, logger, []int64{2 * 60 * 60 * 1000}, nil)
	if err != nil {
		return fmt.Errorf("creating TSDB compactor: %w", err)
	}

	blockULID, err := compactor.Write(stagingDir, head, head.MinTime(), head.MaxTime()+1, nil)
	if err != nil {
		return fmt.Errorf("writing TSDB block: %w", err)
	}

	stagingBlockDir := filepath.Join(stagingDir, blockULID.String())
	err = writeBlockMeta(stagingBlockDir, thanosMeta{
		Labels: map[string]string{
			"job":      job,
			"prow_job": prowJob,
		},
		Builds: []string{buildStr},
		Source: "perfdash-ingest",
	})
	if err != nil {
		return err
	}

	finalBlockDir := filepath.Join(tsdbDirAbs, blockULID.String())
	if err := os.Rename(stagingBlockDir, finalBlockDir); err != nil {
		return fmt.Errorf("moving staged block to %s: %w", finalBlockDir, err)
	}

	ti.ingestedBuilds[fmt.Sprintf("%s/%d", job, buildNumber)] = true

	klog.Infof("Created Thanos TSDB block for %s build %s: %s (%d samples)", job, buildStr, blockULID.String(), sampleCount)
	return nil
}

// CompactJobBlocks merges all TSDB blocks belonging to targetJob into a single TSDB block
// and prunes builds older than the newest maxBuilds when maxBuilds > 0.
func (ti *ThanosIngester) CompactJobBlocks(ctx context.Context, targetJob string, maxBuilds int) error {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	return ti.compactJobBlocksLocked(ctx, targetJob, maxBuilds)
}

// ThanosClient queries Thanos Query API for perfdash metrics.
type ThanosClient struct {
	queryURL string
	client   *http.Client
}

// NewThanosClient creates a new ThanosClient.
func NewThanosClient(queryURL string) *ThanosClient {
	return &ThanosClient{
		queryURL: strings.TrimRight(queryURL, "/"),
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ServeJobNames serves /jobnames from Thanos.
func (tc *ThanosClient) ServeJobNames(res http.ResponseWriter, req *http.Request) {
	tc.serveDistinctLabel(res, req, "", "job")
}

// ServeCategoryNames serves /metriccategorynames from Thanos.
func (tc *ThanosClient) ServeCategoryNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		return
	}
	tc.serveDistinctLabel(res, req, fmt.Sprintf(`{__name__="perfdash_metric",job=%q}`, jobname), "category")
}

// ServeMetricNames serves /metricnames from Thanos.
func (tc *ThanosClient) ServeMetricNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		return
	}
	tc.serveDistinctLabel(res, req, fmt.Sprintf(`{__name__="perfdash_metric",job=%q,category=%q}`, jobname, categoryname), "metric")
}

// ServeBuildsData serves /buildsdata reconstructed from Thanos PromQL queries.
func (tc *ThanosClient) ServeBuildsData(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		return
	}
	metricname, ok := getURLParam(req, "metricname")
	if !ok || metricname == "" {
		return
	}

	query := fmt.Sprintf(`last_over_time(perfdash_metric{job=%q,category=%q,metric=%q}[10y])`, jobname, categoryname, metricname)
	queryResp, err := tc.QueryPromQL(query)
	if err != nil {
		klog.Errorf("Thanos ServeBuildsData query failed: %v", err)
		serveHTTPObject(res, req, &BuildData{Job: jobname, Version: "v1", Builds: NewBuilds(nil)})
		return
	}

	resolvedProwJob := jobname
	buildsMap := make(map[string]map[string]*perftype.DataItem)
	for _, result := range queryResp.Data.Result {
		build := result.Metric["build"]
		stream := result.Metric["percentile"]
		if build == "" || stream == "" {
			continue
		}
		if pj := result.Metric["prow_job"]; pj != "" {
			resolvedProwJob = pj
		}

		otherLabels := decodeSeriesLabels(result.Metric)
		groupKey := createMapID(otherLabels)
		var floatVal float64
		if len(result.Value) >= 2 {
			if strVal, ok := result.Value[1].(string); ok {
				floatVal, _ = strconv.ParseFloat(strVal, 64)
			}
		}

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
		items := make([]perftype.DataItem, 0, len(groupMap))
		for _, item := range groupMap {
			items = append(items, *item)
		}
		resultBuilds[build] = items
	}

	serveHTTPObject(res, req, &BuildData{
		Job:     resolvedProwJob,
		Version: "v1",
		Builds:  NewBuilds(resultBuilds),
	})
}

// QueryLabelValues queries /api/v1/label/<labelName>/values with an optional match[] selector.
func (tc *ThanosClient) QueryLabelValues(matchExpr, labelKey string) ([]string, error) {
	u := fmt.Sprintf("%s/api/v1/label/%s/values?start=0&end=9999999999",
		tc.queryURL, url.PathEscape(labelKey))
	if matchExpr != "" {
		u += "&match[]=" + url.QueryEscape(matchExpr)
	}
	resp, err := tc.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying thanos label values: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading label values response: %w", err)
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
func (tc *ThanosClient) QueryPromQL(queryStr string) (*promQueryResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", tc.queryURL, url.QueryEscape(queryStr))
	resp, err := tc.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying thanos promql: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading promql response: %w", err)
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
	out, err := tc.QueryLabelValues(matchExpr, labelKey)
	if err != nil {
		klog.Errorf("Thanos label values query (%s) failed: %v", matchExpr, err)
		serveHTTPObject(res, req, []string{})
		return
	}
	serveHTTPObject(res, req, &out)
}

type promLabelValuesResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
	Error  string   `json:"error,omitempty"`
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
	var metaObj map[string]interface{}
	if err := json.Unmarshal(metaBytes, &metaObj); err != nil {
		return fmt.Errorf("unmarshaling meta.json: %w", err)
	}
	metaObj["thanos"] = meta
	updatedBytes, err := json.MarshalIndent(metaObj, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling meta.json: %w", err)
	}
	if err := os.WriteFile(metaPath, updatedBytes, 0644); err != nil {
		return fmt.Errorf("writing meta.json: %w", err)
	}
	return nil
}

func (ti *ThanosIngester) loadExistingBlocksLocked() map[string]int {
	jobCounts := make(map[string]int)
	files, err := os.ReadDir(ti.tsdbDir)
	if err != nil {
		return jobCounts
	}
	for _, file := range files {
		if !file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}
		meta, err := readBlockMeta(filepath.Join(ti.tsdbDir, file.Name()))
		if err != nil {
			continue
		}
		job := meta.Thanos.Labels["job"]
		if job == "" {
			continue
		}
		jobCounts[job]++
		for _, b := range meta.Thanos.Builds {
			if b != "" {
				ti.ingestedBuilds[fmt.Sprintf("%s/%s", job, b)] = true
			}
		}
	}
	return jobCounts
}

func (ti *ThanosIngester) compactJobBlocksLocked(ctx context.Context, targetJob string, maxBuilds int) error {
	tsdbDirAbs, err := filepath.Abs(ti.tsdbDir)
	if err != nil {
		return err
	}
	files, err := os.ReadDir(tsdbDirAbs)
	if err != nil {
		return err
	}

	var blocks []jobBlockInfo
	buildSet := make(map[string]bool)
	prowJob := targetJob

	for _, file := range files {
		if !file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}
		blockDir := filepath.Join(tsdbDirAbs, file.Name())
		meta, err := readBlockMeta(blockDir)
		if err != nil || meta.Thanos.Labels["job"] != targetJob {
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
		})
	}

	logger := log.NewNopLogger()
	needsTombstoneCleanup := false
	if maxBuilds > 0 && len(buildSet) > maxBuilds {
		expiredSet := findExpiredBuilds(buildSet, maxBuilds)
		var keptBlocks []jobBlockInfo
		for _, b := range blocks {
			var expiredInBlock []string
			for _, buildStr := range b.builds {
				if expiredSet[buildStr] {
					expiredInBlock = append(expiredInBlock, buildStr)
				}
			}
			if len(expiredInBlock) > 0 && len(expiredInBlock) == len(b.builds) {
				_ = os.RemoveAll(b.dir)
				continue
			}
			if len(expiredInBlock) > 0 {
				pb, err := tsdb.OpenBlock(logger, b.dir, nil)
				if err != nil {
					return fmt.Errorf("opening block %s to prune expired builds: %w", b.dir, err)
				}
				for _, exp := range expiredInBlock {
					if err := pb.Delete(pb.MinTime(), pb.MaxTime(), labels.NewEqualMatcher("build", exp)); err != nil {
						_ = pb.Close()
						return fmt.Errorf("deleting expired build %s from %s: %w", exp, b.dir, err)
					}
				}
				_ = pb.Close()
				needsTombstoneCleanup = true
			}
			keptBlocks = append(keptBlocks, b)
		}
		blocks = keptBlocks
		for exp := range expiredSet {
			delete(buildSet, exp)
			delete(ti.ingestedBuilds, fmt.Sprintf("%s/%s", targetJob, exp))
		}
	}

	if len(blocks) == 0 || (len(blocks) == 1 && !needsTombstoneCleanup) {
		return nil
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].minTime < blocks[j].minTime
	})

	stagingDir, err := os.MkdirTemp(tsdbDirAbs, ".staging-compact-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingDir)

	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, logger, []int64{10 * 365 * 24 * 60 * 60 * 1000}, nil)
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
			return fmt.Errorf("cleaning tombstones for %s: %w", targetJob, err)
		}
		currentDirs = []string{filepath.Join(stagingDir, uid.String())}
	}

	const batchSize = 25
	for len(currentDirs) > 1 {
		var nextDirs []string
		for i := 0; i < len(currentDirs); i += batchSize {
			end := i + batchSize
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
				return fmt.Errorf("compacting batch for %s: %w", targetJob, err)
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
			"job":      targetJob,
			"prow_job": prowJob,
		},
		Builds: allBuilds,
		Source: "perfdash-ingest",
	})
	if err != nil {
		return err
	}

	finalBlockDir := filepath.Join(tsdbDirAbs, finalULID)
	if err := os.Rename(compactedBlockDir, finalBlockDir); err != nil {
		return err
	}

	for _, orig := range blocks {
		_ = os.RemoveAll(orig.dir)
	}

	klog.Infof("Compacted %d blocks for job %s into %s (%d builds)", len(blocks), targetJob, finalULID, len(allBuilds))
	return nil
}

func findExpiredBuilds(buildSet map[string]bool, maxBuilds int) map[string]bool {
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
	for _, b := range all[maxBuilds:] {
		expired[b] = true
	}
	return expired
}

// emptyLabelValueSentinel preserves empty label values (such as Subresource="")
// across Prometheus TSDB, which otherwise drops labels with empty values.
const (
	emptyLabelValueSentinel = "__perfdash_empty__"
	encodedLabelKeyPrefix   = "perfdash_lbl_"
)

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

func encodeSeriesLabels(job, prowJob, category, metricName, buildStr, streamName string, item perftype.DataItem) labels.Labels {
	lblsMap := map[string]string{
		"__name__":   "perfdash_metric",
		"job":        job,
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
