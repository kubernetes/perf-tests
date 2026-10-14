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

// emptyLabelValueSentinel preserves empty label values (such as Subresource="")
// across Prometheus TSDB, which otherwise drops labels with empty values.
const (
	emptyLabelValueSentinel = "__perfdash_empty__"
	encodedLabelKeyPrefix   = "perfdash_lbl_"
)

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

// ThanosIngester writes Prow build metrics into native Thanos TSDB blocks.
type ThanosIngester struct {
	tsdbDir        string
	mu             sync.Mutex
	ingestedBuilds map[string]bool
}

// NewThanosIngester creates a new ThanosIngester and indexes existing TSDB blocks on disk.
func NewThanosIngester(tsdbDir string) *ThanosIngester {
	ti := &ThanosIngester{
		tsdbDir:        tsdbDir,
		ingestedBuilds: make(map[string]bool),
	}
	ti.loadExistingBlocks()
	ti.CompactAllJobs(context.Background())
	return ti
}

func (ti *ThanosIngester) loadExistingBlocks() {
	entries, err := os.ReadDir(ti.tsdbDir)
	if err != nil {
		return
	}
	ti.mu.Lock()
	defer ti.mu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaBytes, err := os.ReadFile(filepath.Join(ti.tsdbDir, entry.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var metaObj struct {
			Thanos struct {
				Labels map[string]string `json:"labels"`
				Builds []string          `json:"builds"`
			} `json:"thanos"`
		}
		if json.Unmarshal(metaBytes, &metaObj) != nil || metaObj.Thanos.Labels == nil {
			continue
		}
		job := metaObj.Thanos.Labels["job"]
		if job == "" {
			continue
		}
		if buildStr := metaObj.Thanos.Labels["build"]; buildStr != "" {
			ti.ingestedBuilds[fmt.Sprintf("%s/%s", job, buildStr)] = true
		}
		for _, b := range metaObj.Thanos.Builds {
			if b != "" {
				ti.ingestedBuilds[fmt.Sprintf("%s/%s", job, b)] = true
			}
		}
	}
}

type jobBlockInfo struct {
	dir     string
	minTime int64
	prowJob string
	builds  []string
}

// CompactAllJobs compacts multiple per-build TSDB blocks into a single TSDB block per job.
func (ti *ThanosIngester) CompactAllJobs(ctx context.Context) {
	entries, err := os.ReadDir(ti.tsdbDir)
	if err != nil {
		return
	}
	jobCounts := make(map[string]int)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaBytes, err := os.ReadFile(filepath.Join(ti.tsdbDir, entry.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var metaObj struct {
			Thanos struct {
				Labels map[string]string `json:"labels"`
			} `json:"thanos"`
		}
		if json.Unmarshal(metaBytes, &metaObj) == nil && metaObj.Thanos.Labels != nil {
			if job := metaObj.Thanos.Labels["job"]; job != "" {
				jobCounts[job]++
			}
		}
	}
	for job, cnt := range jobCounts {
		if cnt > 1 {
			if err := ti.CompactJobBlocks(ctx, job); err != nil {
				klog.Errorf("Compacting blocks for job %s failed: %v", job, err)
			}
		}
	}
}

// CompactJobBlocks merges all TSDB blocks belonging to targetJob into a single TSDB block.
func (ti *ThanosIngester) CompactJobBlocks(ctx context.Context, targetJob string) error {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	tsdbDirAbs, err := filepath.Abs(ti.tsdbDir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(tsdbDirAbs)
	if err != nil {
		return err
	}

	var blocks []jobBlockInfo
	buildSet := make(map[string]bool)
	prowJob := targetJob

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		blockDir := filepath.Join(tsdbDirAbs, entry.Name())
		metaBytes, err := os.ReadFile(filepath.Join(blockDir, "meta.json"))
		if err != nil {
			continue
		}
		var metaObj struct {
			MinTime int64 `json:"minTime"`
			Thanos  struct {
				Labels map[string]string `json:"labels"`
				Builds []string          `json:"builds"`
			} `json:"thanos"`
		}
		if json.Unmarshal(metaBytes, &metaObj) != nil || metaObj.Thanos.Labels == nil {
			continue
		}
		if metaObj.Thanos.Labels["job"] != targetJob {
			continue
		}
		if pj := metaObj.Thanos.Labels["prow_job"]; pj != "" {
			prowJob = pj
		}
		var bList []string
		if b := metaObj.Thanos.Labels["build"]; b != "" {
			bList = append(bList, b)
			buildSet[b] = true
		}
		for _, b := range metaObj.Thanos.Builds {
			if b != "" {
				bList = append(bList, b)
				buildSet[b] = true
			}
		}
		blocks = append(blocks, jobBlockInfo{
			dir:     blockDir,
			minTime: metaObj.MinTime,
			prowJob: prowJob,
			builds:  bList,
		})
	}

	if len(blocks) <= 1 {
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

	logger := log.NewNopLogger()
	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, logger, []int64{10 * 365 * 24 * 60 * 60 * 1000}, nil)
	if err != nil {
		return err
	}

	currentDirs := make([]string, len(blocks))
	for i, b := range blocks {
		currentDirs[i] = b.dir
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
	metaPath := filepath.Join(compactedBlockDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var metaObj map[string]interface{}
	if err := json.Unmarshal(metaBytes, &metaObj); err != nil {
		return err
	}

	allBuilds := make([]string, 0, len(buildSet))
	for b := range buildSet {
		allBuilds = append(allBuilds, b)
	}
	sort.Strings(allBuilds)

	metaObj["thanos"] = map[string]interface{}{
		"labels": map[string]string{
			"job":      targetJob,
			"prow_job": prowJob,
		},
		"builds": allBuilds,
		"source": "perfdash-ingest",
	}
	updatedBytes, err := json.MarshalIndent(metaObj, "", "\t")
	if err != nil {
		return err
	}
	if err := os.WriteFile(metaPath, updatedBytes, 0644); err != nil {
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
					lset := labels.FromMap(lblsMap)
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
	metaPath := filepath.Join(stagingBlockDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("reading meta.json: %w", err)
	}
	var metaObj map[string]interface{}
	if err := json.Unmarshal(metaBytes, &metaObj); err != nil {
		return fmt.Errorf("unmarshaling meta.json: %w", err)
	}
	metaObj["thanos"] = map[string]interface{}{
		"labels": map[string]string{
			"job":      job,
			"prow_job": prowJob,
			"build":    buildStr,
		},
		"source": "perfdash-ingest",
	}
	updatedBytes, err := json.MarshalIndent(metaObj, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling meta.json: %w", err)
	}
	if err := os.WriteFile(metaPath, updatedBytes, 0644); err != nil {
		return fmt.Errorf("writing meta.json: %w", err)
	}

	finalBlockDir := filepath.Join(tsdbDirAbs, blockULID.String())
	if err := os.Rename(stagingBlockDir, finalBlockDir); err != nil {
		return fmt.Errorf("moving staged block to %s: %w", finalBlockDir, err)
	}

	ti.ingestedBuilds[fmt.Sprintf("%s/%d", job, buildNumber)] = true

	klog.Infof("Created Thanos TSDB block for %s build %s: %s (%d samples)", job, buildStr, blockULID.String(), sampleCount)
	return nil
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

		otherLabels := make(map[string]string)
		for k, v := range result.Metric {
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
