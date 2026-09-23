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

const (
	emptyLabelValueSentinel = "__perfdash_empty__"
	encodedLabelPrefix      = "perfdash_lbl_"
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

func isValidPromLabelName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || (b >= '0' && b <= '9' && i > 0)) {
			return false
		}
	}
	return true
}

func encodeItemLabelKey(k string) string {
	if reservedTSDBLabels[k] || strings.HasPrefix(k, encodedLabelPrefix) || !isValidPromLabelName(k) {
		return encodedLabelPrefix + hex.EncodeToString([]byte(k))
	}
	return k
}

func decodeItemLabelKey(k string) string {
	if strings.HasPrefix(k, encodedLabelPrefix) {
		raw, err := hex.DecodeString(strings.TrimPrefix(k, encodedLabelPrefix))
		if err == nil {
			return string(raw)
		}
	}
	return k
}

func encodeItemLabelValue(v string) string {
	if v == "" {
		return emptyLabelValueSentinel
	}
	return v
}

func decodeItemLabelValue(v string) string {
	if v == emptyLabelValueSentinel {
		return ""
	}
	return v
}

// ThanosIngester writes Prow build metrics into native Thanos TSDB blocks.
type ThanosIngester struct {
	tsdbDir        string
	mu             sync.Mutex
	ingestedBuilds map[string]bool
}

// NewThanosIngester creates a new ThanosIngester and indexes any existing TSDB blocks on disk.
func NewThanosIngester(tsdbDir string) *ThanosIngester {
	ti := &ThanosIngester{
		tsdbDir:        tsdbDir,
		ingestedBuilds: make(map[string]bool),
	}
	ti.loadExistingBlocks()
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
		metaPath := filepath.Join(ti.tsdbDir, entry.Name(), "meta.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var metaObj struct {
			Thanos struct {
				Labels map[string]string `json:"labels"`
			} `json:"thanos"`
		}
		if json.Unmarshal(metaBytes, &metaObj) != nil || metaObj.Thanos.Labels == nil {
			continue
		}
		buildStr := metaObj.Thanos.Labels["build"]
		if buildStr == "" {
			continue
		}
		if job := metaObj.Thanos.Labels["job"]; job != "" {
			ti.ingestedBuilds[fmt.Sprintf("%s/%s", job, buildStr)] = true
		}
		if prowJob := metaObj.Thanos.Labels["prow_job"]; prowJob != "" {
			ti.ingestedBuilds[fmt.Sprintf("%s/%s", prowJob, buildStr)] = true
		}
	}
}

// HasBuild reports whether the specified job and build number have already been ingested.
func (ti *ThanosIngester) HasBuild(job string, buildNumber int) bool {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	return ti.ingestedBuilds[fmt.Sprintf("%s/%d", job, buildNumber)]
}

// markBuildIngested records that a build was written to TSDB.
func (ti *ThanosIngester) markBuildIngested(job, prowJob string, buildNumber int) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	if job != "" {
		ti.ingestedBuilds[fmt.Sprintf("%s/%d", job, buildNumber)] = true
	}
	if prowJob != "" {
		ti.ingestedBuilds[fmt.Sprintf("%s/%d", prowJob, buildNumber)] = true
	}
}

// IngestBuild writes metric samples for a single build into a native Thanos TSDB block.
// Blocks are staged in a hidden directory with Thanos metadata injected into meta.json before
// being atomically renamed into tsdbDir so Thanos Store never observes an incomplete block.
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
						lblsMap[encodeItemLabelKey(k)] = encodeItemLabelValue(v)
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

	ti.markBuildIngested(job, prowJob, buildNumber)
	klog.Infof("Created Thanos TSDB block for %s (prowJob=%s) build %s: %s (%d samples)", job, prowJob, buildStr, blockULID.String(), sampleCount)
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

type promSeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
	Error  string              `json:"error,omitempty"`
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

// QueryLabelValues queries /api/v1/label/<labelName>/values with match[] selector,
// avoiding full-series serialization over HTTP.
func (tc *ThanosClient) QueryLabelValues(matchExpr, labelKey string) ([]string, error) {
	u := fmt.Sprintf("%s/api/v1/label/%s/values?match[]=%s&start=0&end=9999999999",
		tc.queryURL, url.PathEscape(labelKey), url.QueryEscape(matchExpr))
	resp, err := tc.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying thanos label values: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from label values endpoint", resp.StatusCode)
	}

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

// QuerySeries queries /api/v1/series for matching series labels.
func (tc *ThanosClient) QuerySeries(matchExpr string) ([]map[string]string, error) {
	u := fmt.Sprintf("%s/api/v1/series?match[]=%s&start=0&end=9999999999", tc.queryURL, url.QueryEscape(matchExpr))
	resp, err := tc.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying thanos series: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading series response: %w", err)
	}

	var seriesResp promSeriesResponse
	if err := json.Unmarshal(body, &seriesResp); err != nil {
		return nil, fmt.Errorf("unmarshaling series response: %w", err)
	}
	if seriesResp.Status != "success" {
		return nil, fmt.Errorf("thanos series error: %s", seriesResp.Error)
	}
	return seriesResp.Data, nil
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

func (tc *ThanosClient) distinctLabelValues(matchExpr, labelKey string) ([]string, error) {
	if vals, err := tc.QueryLabelValues(matchExpr, labelKey); err == nil {
		return vals, nil
	}
	series, err := tc.QuerySeries(matchExpr)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, s := range series {
		if val := s[labelKey]; val != "" {
			seen[val] = true
		}
	}
	out := make([]string, 0, len(seen))
	for val := range seen {
		out = append(out, val)
	}
	sort.Strings(out)
	return out, nil
}

func (tc *ThanosClient) serveDistinctLabel(res http.ResponseWriter, req *http.Request, matchExpr, fallbackMatchExpr, labelKey string) {
	out, err := tc.distinctLabelValues(matchExpr, labelKey)
	if (err != nil || len(out) == 0) && fallbackMatchExpr != "" {
		if fallbackOut, fallbackErr := tc.distinctLabelValues(fallbackMatchExpr, labelKey); fallbackErr == nil && len(fallbackOut) > 0 {
			out = fallbackOut
			err = nil
		}
	}
	if err != nil {
		klog.Errorf("Thanos query series (%s) failed: %v", matchExpr, err)
		serveHTTPObject(res, req, []string{})
		return
	}
	serveHTTPObject(res, req, &out)
}

// ServeJobNames serves /jobnames from Thanos.
func (tc *ThanosClient) ServeJobNames(res http.ResponseWriter, req *http.Request) {
	tc.serveDistinctLabel(res, req, `{__name__="perfdash_metric"}`, "", "job")
}

// ServeCategoryNames serves /metriccategorynames from Thanos.
func (tc *ThanosClient) ServeCategoryNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		serveHTTPObject(res, req, []string{})
		return
	}
	tc.serveDistinctLabel(
		res,
		req,
		fmt.Sprintf(`{__name__="perfdash_metric",job=%q}`, jobname),
		fmt.Sprintf(`{__name__="perfdash_metric",prow_job=%q}`, jobname),
		"category",
	)
}

// ServeMetricNames serves /metricnames from Thanos.
func (tc *ThanosClient) ServeMetricNames(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		serveHTTPObject(res, req, []string{})
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		serveHTTPObject(res, req, []string{})
		return
	}
	tc.serveDistinctLabel(
		res,
		req,
		fmt.Sprintf(`{__name__="perfdash_metric",job=%q,category=%q}`, jobname, categoryname),
		fmt.Sprintf(`{__name__="perfdash_metric",prow_job=%q,category=%q}`, jobname, categoryname),
		"metric",
	)
}

// ServeBuildsData serves /buildsdata reconstructed from Thanos PromQL queries.
func (tc *ThanosClient) ServeBuildsData(res http.ResponseWriter, req *http.Request) {
	jobname, ok := getURLParam(req, "jobname")
	if !ok || jobname == "" {
		serveHTTPObject(res, req, &BuildData{Version: "v1", Builds: NewBuilds(nil)})
		return
	}
	categoryname, ok := getURLParam(req, "metriccategoryname")
	if !ok || categoryname == "" {
		serveHTTPObject(res, req, &BuildData{Job: jobname, Version: "v1", Builds: NewBuilds(nil)})
		return
	}
	metricname, ok := getURLParam(req, "metricname")
	if !ok || metricname == "" {
		serveHTTPObject(res, req, &BuildData{Job: jobname, Version: "v1", Builds: NewBuilds(nil)})
		return
	}

	query := fmt.Sprintf(`last_over_time(perfdash_metric{job=%q,category=%q,metric=%q}[10y])`, jobname, categoryname, metricname)
	queryResp, err := tc.QueryPromQL(query)
	if err == nil && len(queryResp.Data.Result) == 0 {
		fallbackQuery := fmt.Sprintf(`last_over_time(perfdash_metric{prow_job=%q,category=%q,metric=%q}[10y])`, jobname, categoryname, metricname)
		if fallbackResp, fallbackErr := tc.QueryPromQL(fallbackQuery); fallbackErr == nil && len(fallbackResp.Data.Result) > 0 {
			queryResp = fallbackResp
		}
	}
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
			otherLabels[decodeItemLabelKey(k)] = decodeItemLabelValue(v)
		}

		groupKey := createMapID(otherLabels) + "|unit:" + result.Metric["unit"]
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
		Job:     resolvedProwJob,
		Version: "v1",
		Builds:  NewBuilds(resultBuilds),
	})
}

