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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

// -----------------------------------------------------------------------------
// Helper Logger
// -----------------------------------------------------------------------------

type nopLogger struct{}

func (nopLogger) Log(keyvals ...interface{}) error {
	return nil
}

// -----------------------------------------------------------------------------
// Configuration & Constants
// -----------------------------------------------------------------------------

var sloTargetsMS = map[string]float64{
	"API Call Latency (Mutating) [s]":       1000.0,  // 1.0s
	"API Call Latency (Read-Only) [s]":      1000.0,  // 1.0s
	"API Call Latency (List Cluster) [s]":   30000.0, // 30.0s
	"API Call Latency (List Namespace) [s]": 5000.0,  // 5.0s
	"Pod Startup Latency [s]":               5000.0,  // 5.0s
	"DNS Lookup Latency [s]":                5000.0,  // 5.0s
}

type SLOEntry struct {
	SLO       string
	Resource  string
	Verb      string
	Target    string
	Actual    string
	TargetVal float64
	ActualVal float64
	Status    int
}

type APIAvailabilityFile struct {
	ClusterMetrics struct {
		AvailabilityPercentage float64 `json:"availabilityPercentage"`
	} `json:"clusterMetrics"`
}

type SchedulingThroughputFile struct {
	Max float64 `json:"max"`
}

// -----------------------------------------------------------------------------
// Main Execution (Top Abstraction Level)
// -----------------------------------------------------------------------------

func main() {
	opts := parseFlags()

	rawBuildID := strings.TrimPrefix(opts.buildID, "run-")
	runName := "run-" + rawBuildID

	runDir := filepath.Join(opts.artifactsDir, "runs", rawBuildID)
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Run directory does not exist at %s\n", runDir)
		os.Exit(1)
	}

	ingestors, err := getIngestors(opts.mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting ingestors: %v\n", err)
		os.Exit(1)
	}

	var valErrors []string
	for _, ing := range ingestors {
		if err := ing.Validate(rawBuildID, runName, opts.artifactsDir); err != nil {
			valErrors = append(valErrors, fmt.Sprintf("%v", err))
		}
	}
	if len(valErrors) > 0 {
		fmt.Fprintf(os.Stderr, "Pre-validation error for build %s:\n  missing artifact(s): %s\n", opts.buildID, strings.Join(valErrors, "; "))
		os.Exit(1)
	}

	for _, ing := range ingestors {
		if err := ing.Ingest(rawBuildID, runName, opts.artifactsDir); err != nil {
			fmt.Fprintf(os.Stderr, "Ingestion error for build %s [%s]: %v\n", opts.buildID, ing.Name(), err)
			os.Exit(1)
		}
	}
}

// -----------------------------------------------------------------------------
// Metric Ingestion Operations
// -----------------------------------------------------------------------------

func ingestBuildMetrics(buildID string, artifactsDir string) ([]SLOEntry, error) {
	files, err := findCL2JSONFiles(buildID, artifactsDir)
	if err != nil {
		return nil, fmt.Errorf("finding local artifact files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no CL2 metric JSON files found locally for Build ID %s", buildID)
	}

	var allEntries []SLOEntry

	for _, file := range files {
		filename := filepath.Base(file)
		body, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("Warning: Skipping %s due to read error: %v\n", file, err)
			continue
		}

		if strings.HasPrefix(filename, "PodStartupLatency_PodStartupLatency_load_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluatePodStartupLatency(extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "APIResponsivenessPrometheus_load_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluateAPIResponsiveness(extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "DnsLookupLatency_load_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluateGenericLatency("DNS Lookup Latency [s]", extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "APIAvailability_load_") {
			var avail APIAvailabilityFile
			if err := json.Unmarshal(body, &avail); err == nil {
				actualPct := avail.ClusterMetrics.AvailabilityPercentage
				if actualPct > 0.0 {
					status := 0
					if actualPct >= 99.50 {
						status = 1
					}
					allEntries = append(allEntries, SLOEntry{
						SLO:       "API Availability [%]",
						Resource:  "",
						Verb:      "",
						Target:    "99.50",
						Actual:    fmt.Sprintf("%.2f", actualPct),
						TargetVal: 99.50,
						ActualVal: actualPct,
						Status:    status,
					})
				}
			}
		} else if strings.HasPrefix(filename, "SchedulingThroughputPrometheus_load_") {
			var st SchedulingThroughputFile
			if err := json.Unmarshal(body, &st); err == nil {
				if st.Max > 0.0 {
					status := 0
					if st.Max >= 100.0 {
						status = 1
					}
					allEntries = append(allEntries, SLOEntry{
						SLO:       "Scheduling Throughput [pods/s]",
						Resource:  "",
						Verb:      "",
						Target:    "100.00",
						Actual:    fmt.Sprintf("%.1f", st.Max),
						TargetVal: 100.0,
						ActualVal: st.Max,
						Status:    status,
					})
				}
			}
		}
	}

	return allEntries, nil
}

func generateOpenMetricsFile(runName string, entries []SLOEntry, omDir string) (string, error) {
	if err := os.MkdirAll(omDir, 0755); err != nil {
		return "", err
	}
	omFile := filepath.Join(omDir, fmt.Sprintf("openmetrics_%s.txt", runName))

	timestampSec := time.Now().UTC().Unix()
	var builder strings.Builder
	builder.WriteString("# HELP k8s_slo_status Status of K8s SLO evaluation (1 = PASS, 0 = FAIL)\n")
	builder.WriteString("# TYPE k8s_slo_status gauge\n")
	builder.WriteString("# HELP k8s_slo_measurement Measured numeric value of K8s SLO evaluation\n")
	builder.WriteString("# TYPE k8s_slo_measurement gauge\n")
	builder.WriteString("# HELP k8s_slo_target Target threshold numeric value of K8s SLO evaluation\n")
	builder.WriteString("# TYPE k8s_slo_target gauge\n")

	for _, e := range entries {
		builder.WriteString(fmt.Sprintf(
			`k8s_slo_status{run="%s",slo="%s",resource="%s",verb="%s",target="%s",actual="%s"} %d %d`+"\n",
			runName, e.SLO, e.Resource, e.Verb, e.Target, e.Actual, e.Status, timestampSec,
		))
		builder.WriteString(fmt.Sprintf(
			`k8s_slo_measurement{run="%s",slo="%s",resource="%s",verb="%s"} %f %d`+"\n",
			runName, e.SLO, e.Resource, e.Verb, e.ActualVal, timestampSec,
		))
		builder.WriteString(fmt.Sprintf(
			`k8s_slo_target{run="%s",slo="%s",resource="%s",verb="%s"} %f %d`+"\n",
			runName, e.SLO, e.Resource, e.Verb, e.TargetVal, timestampSec,
		))
	}
	builder.WriteString("# EOF\n")

	if err := os.WriteFile(omFile, []byte(builder.String()), 0644); err != nil {
		return "", err
	}

	fmt.Printf("Generated OpenMetrics artifact for %s: %s (%d SLO entries)\n", runName, omFile, len(entries))
	return omFile, nil
}

func createThanosTSDBBlock(tsdbDir string, runName string, entries []SLOEntry) error {
	if err := os.MkdirAll(tsdbDir, 0755); err != nil {
		return err
	}

	tsdbDirAbs, err := filepath.Abs(tsdbDir)
	if err != nil {
		return err
	}

	fmt.Println("Writing TSDB block and injecting Thanos metadata via github.com/thanos-io/thanos Go package...")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bw, err := tsdb.NewBlockWriter(logger, tsdbDirAbs, 2*60*60*1000) // 2 hour block size
	if err != nil {
		return fmt.Errorf("creating TSDB BlockWriter: %w", err)
	}
	defer bw.Close()

	ctx := context.Background()
	app := bw.Appender(ctx)
	timestampMS := time.Now().UTC().UnixMilli()

	for _, e := range entries {
		lblsStatus := labels.FromMap(map[string]string{
			"__name__": "k8s_slo_status",
			"run":      runName,
			"slo":      e.SLO,
			"resource": e.Resource,
			"verb":     e.Verb,
			"target":   e.Target,
			"actual":   e.Actual,
		})
		if _, err := app.Append(0, lblsStatus, timestampMS, float64(e.Status)); err != nil {
			return fmt.Errorf("appending status sample to TSDB: %w", err)
		}

		lblsBase := map[string]string{
			"run":      runName,
			"slo":      e.SLO,
			"resource": e.Resource,
			"verb":     e.Verb,
		}

		lblsMeas := labels.FromMap(mergeMap(lblsBase, map[string]string{"__name__": "k8s_slo_measurement"}))
		if _, err := app.Append(0, lblsMeas, timestampMS, e.ActualVal); err != nil {
			return fmt.Errorf("appending measurement sample to TSDB: %w", err)
		}

		lblsTarget := labels.FromMap(mergeMap(lblsBase, map[string]string{"__name__": "k8s_slo_target"}))
		if _, err := app.Append(0, lblsTarget, timestampMS, e.TargetVal); err != nil {
			return fmt.Errorf("appending target sample to TSDB: %w", err)
		}
	}

	if err := app.Commit(); err != nil {
		return fmt.Errorf("committing TSDB appender: %w", err)
	}

	blockULID, err := bw.Flush(ctx)
	if err != nil {
		return fmt.Errorf("writing TSDB block: %w", err)
	}

	// Thanos Metadata Injection
	blockDir := filepath.Join(tsdbDirAbs, blockULID.String())
	thanosMeta := metadata.Thanos{
		Labels: map[string]string{
			"run": runName,
		},
		Source: metadata.SourceType("perflens-ingest"),
	}

	oldLogger := nopLogger{}
	if _, err := metadata.InjectThanos(oldLogger, blockDir, thanosMeta, nil); err != nil {
		return fmt.Errorf("injecting Thanos metadata into block %s: %w", blockULID.String(), err)
	}

	fmt.Printf("Native Thanos TSDB block created & injected successfully: ULID %s in %s\n", blockULID.String(), tsdbDirAbs)
	return nil
}

func ingestPrometheusSnapshot(buildID string, runName string, artifactsDir string, tsdbDir string) error {
	rawBuildID := strings.TrimPrefix(buildID, "run-")
	runDir := filepath.Join(artifactsDir, "runs", rawBuildID)

	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		return fmt.Errorf("no run directory found at %s", runDir)
	}

	foundBlocks := 0
	err := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		metaPath := filepath.Join(path, "meta.json")
		indexPath := filepath.Join(path, "index")
		if _, e1 := os.Stat(metaPath); e1 == nil {
			if _, e2 := os.Stat(indexPath); e2 == nil {
				blockULID := filepath.Base(path)
				targetDir := filepath.Join(tsdbDir, blockULID)

				if err := copyDir(path, targetDir); err != nil {
					return nil
				}
				if err := injectThanosRunLabel(targetDir, runName); err != nil {
					fmt.Printf("Warning: Failed injecting Thanos metadata into %s: %v\n", blockULID, err)
				}
				foundBlocks++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if foundBlocks == 0 {
		return fmt.Errorf("no Prometheus snapshot TSDB blocks found in %s", runDir)
	}

	fmt.Printf("Successfully ingested %d Prometheus snapshot TSDB blocks for %s\n", foundBlocks, runName)
	return nil
}

func mergeMap(m1, m2 map[string]string) map[string]string {
	res := make(map[string]string, len(m1)+len(m2))
	for k, v := range m1 {
		res[k] = v
	}
	for k, v := range m2 {
		res[k] = v
	}
	return res
}

// -----------------------------------------------------------------------------
// Low-Level Helper & Detail Functions
// -----------------------------------------------------------------------------

func evaluatePodStartupLatency(items []MetricItem) []SLOEntry {
	var entries []SLOEntry
	sloCat := "Pod Startup Latency [s]"
	targetMS := sloTargetsMS[sloCat]
	targetSec := targetMS / 1000.0

	for _, item := range items {
		perc99MS := parseLatencyMS(item)
		if perc99MS <= 0.0 {
			continue
		}
		actualSec := perc99MS / 1000.0
		status := 0
		if perc99MS <= targetMS {
			status = 1
		}

		resource := item.Labels["Resource"]
		verb := item.Labels["Verb"]

		entries = append(entries, SLOEntry{
			SLO:       sloCat,
			Resource:  resource,
			Verb:      verb,
			Target:    fmt.Sprintf("%.2f", targetSec),
			Actual:    fmt.Sprintf("%.2f", actualSec),
			TargetVal: targetSec,
			ActualVal: actualSec,
			Status:    status,
		})
	}
	return entries
}

func evaluateGenericLatency(sloCat string, items []MetricItem) []SLOEntry {
	var entries []SLOEntry
	targetMS, targetExists := sloTargetsMS[sloCat]
	if !targetExists {
		return nil
	}
	targetSec := targetMS / 1000.0

	for _, item := range items {
		perc99MS := parseLatencyMS(item)
		if perc99MS <= 0.0 {
			continue
		}
		actualSec := perc99MS / 1000.0
		resource := item.Labels["Resource"]
		verb := item.Labels["Verb"]

		status := 0
		if perc99MS <= targetMS {
			status = 1
		}

		entries = append(entries, SLOEntry{
			SLO:       sloCat,
			Resource:  resource,
			Verb:      verb,
			Target:    fmt.Sprintf("%.2f", targetSec),
			Actual:    fmt.Sprintf("%.2f", actualSec),
			TargetVal: targetSec,
			ActualVal: actualSec,
			Status:    status,
		})
	}
	return entries
}

func evaluateAPIResponsiveness(items []MetricItem) []SLOEntry {
	var entries []SLOEntry
	for _, item := range items {
		sloCat := categorizeEntry(item.Labels)
		if sloCat == "" {
			continue
		}

		targetMS, targetExists := sloTargetsMS[sloCat]
		if !targetExists {
			continue
		}

		perc99MS := parseLatencyMS(item)
		if perc99MS <= 0.0 {
			continue
		}

		resource := item.Labels["Resource"]
		subresource := item.Labels["Subresource"]
		if subresource != "" {
			resource = fmt.Sprintf("%s/%s", resource, subresource)
		}

		verb := item.Labels["Verb"]
		actualSec := perc99MS / 1000.0
		targetSec := targetMS / 1000.0
		status := 0
		if perc99MS <= targetMS {
			status = 1
		}

		entries = append(entries, SLOEntry{
			SLO:       sloCat,
			Resource:  resource,
			Verb:      verb,
			Target:    fmt.Sprintf("%.2f", targetSec),
			Actual:    fmt.Sprintf("%.2f", actualSec),
			TargetVal: targetSec,
			ActualVal: actualSec,
			Status:    status,
		})
	}
	return entries
}

func categorizeEntry(labels map[string]string) string {
	verb := strings.ToUpper(labels["Verb"])
	scope := strings.ToLower(labels["Scope"])
	resource := strings.ToLower(labels["Resource"])
	subresource := strings.ToLower(labels["Subresource"])

	// Exclude non-API resources (e.g. empty, unknown, healthz, livez, readyz, token, watchlist)
	if resource == "" || resource == "unknown" || strings.HasPrefix(resource, "/") || strings.Contains(resource, "unknown") || strings.Contains(resource, "//") {
		return ""
	}
	if subresource == "token" || strings.Contains(resource, "token") || strings.Contains(resource, "health") || strings.Contains(resource, "readyz") || strings.Contains(resource, "livez") || strings.Contains(subresource, "health") || strings.Contains(subresource, "readyz") || strings.Contains(subresource, "livez") {
		return ""
	}
	if verb == "WATCHLIST" || verb == "" {
		return ""
	}

	switch verb {
	case "POST", "PUT", "PATCH", "DELETE", "APPLY":
		return "API Call Latency (Mutating) [s]"
	case "GET":
		return "API Call Latency (Read-Only) [s]"
	case "LIST":
		if scope == "cluster" || scope == "global" {
			return "API Call Latency (List Cluster) [s]"
		} else if scope == "namespace" {
			return "API Call Latency (List Namespace) [s]"
		}
	}
	return ""
}

func parseLatencyMS(item MetricItem) float64 {
	perc99MS := item.Data.Perc99
	switch item.Unit {
	case "s":
		perc99MS *= 1000.0
	case "us":
		perc99MS /= 1000.0
	}
	return perc99MS
}

type MetricItem struct {
	Data   MetricData        `json:"data"`
	Unit   string            `json:"unit"`
	Labels map[string]string `json:"labels"`
}

type MetricData struct {
	Perc50 float64 `json:"Perc50"`
	Perc90 float64 `json:"Perc90"`
	Perc99 float64 `json:"Perc99"`
}

type MetricFile struct {
	Version   string       `json:"version"`
	DataItems []MetricItem `json:"dataItems"`
	Data      []MetricItem `json:"data"`
}

func extractItems(file *MetricFile) []MetricItem {
	if file == nil {
		return nil
	}
	if len(file.DataItems) > 0 {
		return file.DataItems
	}
	return file.Data
}

func findCL2JSONFiles(buildID string, artifactsDir string) ([]string, error) {
	rawBuildID := strings.TrimPrefix(buildID, "run-")
	runDir := filepath.Join(artifactsDir, "runs", rawBuildID)

	var jsonFiles []string
	err := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".json") && (strings.Contains(info.Name(), "APIResponsivenessPrometheus_load_") ||
			strings.Contains(info.Name(), "PodStartupLatency_PodStartupLatency_load_") ||
			strings.Contains(info.Name(), "APIAvailability_load_") ||
			strings.Contains(info.Name(), "SchedulingThroughputPrometheus_load_") ||
			strings.Contains(info.Name(), "DnsLookupLatency_load_")) {
			jsonFiles = append(jsonFiles, path)
		}
		return nil
	})
	return jsonFiles, err
}

func copyDir(src, dst string) error {
	if src == dst {
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		return copyFile(path, targetPath)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func injectThanosRunLabel(blockDir string, runName string) error {
	thanosMeta := metadata.Thanos{
		Labels: map[string]string{
			"run": runName,
		},
		Source: metadata.SourceType("perflens-snapshot-ingest"),
	}

	oldLogger := nopLogger{}
	_, err := metadata.InjectThanos(oldLogger, blockDir, thanosMeta, nil)
	return err
}

func findPrometheusSnapshotBlocks(buildID string, artifactsDir string) ([]string, error) {
	rawBuildID := strings.TrimPrefix(buildID, "run-")
	runDir := filepath.Join(artifactsDir, "runs", rawBuildID)

	var blockDirs []string
	err := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		metaPath := filepath.Join(path, "meta.json")
		indexPath := filepath.Join(path, "index")
		if _, e1 := os.Stat(metaPath); e1 == nil {
			if _, e2 := os.Stat(indexPath); e2 == nil {
				blockDirs = append(blockDirs, path)
			}
		}
		return nil
	})
	return blockDirs, err
}

func getIngestors(mode string) ([]Ingestor, error) {
	var ingestors []Ingestor
	switch strings.ToLower(mode) {
	case "slo", "slo-metrics":
		ingestors = append(ingestors, &sloIngestor{})
	case "prometheus", "prometheus-metrics":
		ingestors = append(ingestors, &prometheusIngestor{})
	case "all":
		ingestors = append(ingestors, &sloIngestor{}, &prometheusIngestor{})
	default:
		return nil, fmt.Errorf("unsupported ingestion mode: %s", mode)
	}
	return ingestors, nil
}

type Ingestor interface {
	Name() string
	Validate(buildID, runName, artifactsDir string) error
	Ingest(buildID, runName, artifactsDir string) error
}

type sloIngestor struct{}

func (s *sloIngestor) Name() string {
	return "slo-metrics"
}

func (s *sloIngestor) Validate(buildID, runName, artifactsDir string) error {
	files, err := findCL2JSONFiles(buildID, artifactsDir)
	if err != nil || len(files) == 0 {
		return fmt.Errorf("CL2 SLO metric JSON files (e.g. APIResponsivenessPrometheus_load_*.json)")
	}
	return nil
}

func (s *sloIngestor) Ingest(buildID, runName, artifactsDir string) error {
	entries, err := ingestBuildMetrics(buildID, artifactsDir)
	if err != nil {
		return fmt.Errorf("reading SLO metrics: %w", err)
	}

	omDir := filepath.Join(artifactsDir, "openmetrics")
	tsdbDir := filepath.Join(artifactsDir, "prometheus")

	if _, err := generateOpenMetricsFile(runName, entries, omDir); err != nil {
		return fmt.Errorf("generating OpenMetrics file: %w", err)
	}

	if err := createThanosTSDBBlock(tsdbDir, runName, entries); err != nil {
		return fmt.Errorf("creating Thanos TSDB block: %w", err)
	}

	return nil
}

type prometheusIngestor struct{}

func (p *prometheusIngestor) Name() string {
	return "prometheus-metrics"
}

func (p *prometheusIngestor) Validate(buildID, runName, artifactsDir string) error {
	blocks, err := findPrometheusSnapshotBlocks(buildID, artifactsDir)
	if err != nil || len(blocks) == 0 {
		return fmt.Errorf("Prometheus snapshot TSDB blocks (folders containing meta.json and index)")
	}
	return nil
}

func (p *prometheusIngestor) Ingest(buildID, runName, artifactsDir string) error {
	tsdbDir := filepath.Join(artifactsDir, "prometheus")
	return ingestPrometheusSnapshot(buildID, runName, artifactsDir, tsdbDir)
}

type options struct {
	buildID      string
	mode         string
	artifactsDir string
}

func parseFlags() options {
	buildID := flag.String("build-id", "", "Prow Build ID or run identifier")
	mode := flag.String("mode", "all", "Ingestion mode: slo-metrics, prometheus-metrics, or all")
	artifactsDir := flag.String("artifacts-dir", "", "Root _artifacts directory")
	flag.Parse()

	if *buildID == "" || *artifactsDir == "" {
		flag.Usage()
		os.Exit(1)
	}
	return options{
		buildID:      *buildID,
		mode:         *mode,
		artifactsDir: *artifactsDir,
	}
}
