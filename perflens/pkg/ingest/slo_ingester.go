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

package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

// SLOIngester processes ClusterLoader2 summary JSON files and ingests SLO metrics into Thanos TSDB.
type SLOIngester struct{}

// NewSLOIngester creates a new instance of SLOIngester.
func NewSLOIngester() *SLOIngester {
	return &SLOIngester{}
}

// Ingest implements the ingestion entrypoint for SLO metrics.
func (i *SLOIngester) Ingest(ctx context.Context, runDir string, runName string, tsdbDir string, omDir string, tb *Timebase) error {
	entries, err := i.ingestBuildMetrics(runDir)
	if err != nil {
		return fmt.Errorf("ingesting SLO metrics from %s: %w", runDir, err)
	}

	if _, err := i.generateOpenMetricsFile(runName, entries, omDir, tb); err != nil {
		return fmt.Errorf("generating OpenMetrics file: %w", err)
	}

	if err := i.createThanosTSDBBlock(ctx, tsdbDir, runName, entries, tb); err != nil {
		return fmt.Errorf("creating Thanos TSDB block: %w", err)
	}

	return nil
}

// SLOEntry represents an evaluated SLO verification metric.
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

func (i *SLOIngester) ingestBuildMetrics(runDir string) ([]SLOEntry, error) {
	files, err := findLocalCL2JSONFiles(runDir)
	if err != nil {
		return nil, fmt.Errorf("finding local artifact files in %s: %w", runDir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no CL2 metric JSON files found in %s", runDir)
	}

	var allEntries []SLOEntry

	for _, file := range files {
		filename := filepath.Base(file)
		body, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("Warning: Skipping %s due to read error: %v\n", file, err)
			continue
		}

		if strings.HasPrefix(filename, "PodStartupLatency_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluatePodStartupLatency(extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "APIResponsivenessPrometheus_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluateAPIResponsiveness(extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "DnsLookupLatency_") {
			var metricFile MetricFile
			if err := json.Unmarshal(body, &metricFile); err == nil {
				allEntries = append(allEntries, evaluateGenericLatency("DNS Lookup Latency [s]", extractItems(&metricFile))...)
			}
		} else if strings.HasPrefix(filename, "APIAvailability_") {
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
		} else if strings.HasPrefix(filename, "SchedulingThroughputPrometheus_") {
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

func (i *SLOIngester) generateOpenMetricsFile(runName string, entries []SLOEntry, omDir string, tb *Timebase) (string, error) {
	if err := os.MkdirAll(omDir, 0755); err != nil {
		return "", err
	}
	omFile := filepath.Join(omDir, fmt.Sprintf("openmetrics_%s.txt", runName))

	// The text artifact is the wall clock view, so it keeps the run's real start
	// time even when the TSDB block is normalized.
	timestampSec := time.Now().UTC().Unix()
	if tb != nil && tb.Known {
		timestampSec = tb.StartMS / 1000
	}
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

// createThanosTSDBBlock writes the run's SLO verdicts as a constant line spanning
// the run's normalized window. An SLO entry summarizes the whole run, so it holds
// over the run's own window rather than being stamped at ingest time. That also
// lands the SLO block on the same axis as the run's snapshot and log blocks.
func (i *SLOIngester) createThanosTSDBBlock(ctx context.Context, tsdbDir string, runName string, entries []SLOEntry, tb *Timebase) error {
	startMS := tb.NormalizedStart()
	endMS := tb.NormalizedEnd()

	fmt.Println("Writing TSDB block and injecting Thanos metadata via github.com/thanos-io/thanos Go package...")
	if _, err := removeRunBlocks(tsdbDir, runName, sourceSLO); err != nil {
		return err
	}

	blockULID, err := writeThanosBlock(ctx, tsdbDir, runName, sourceSLO,
		blockSizeForSpan(startMS, endMS),
		func(bw *tsdb.BlockWriter) error {
			app := bw.Appender(ctx)

			appendAt := func(lbls labels.Labels, value float64) error {
				return appendRunSpan(app, lbls, startMS, endMS, value)
			}

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
				if err := appendAt(lblsStatus, float64(e.Status)); err != nil {
					return fmt.Errorf("appending status sample to TSDB: %w", err)
				}

				lblsBase := map[string]string{
					"run":      runName,
					"slo":      e.SLO,
					"resource": e.Resource,
					"verb":     e.Verb,
				}

				lblsMeas := labels.FromMap(mergeMap(lblsBase, map[string]string{"__name__": "k8s_slo_measurement"}))
				if err := appendAt(lblsMeas, e.ActualVal); err != nil {
					return fmt.Errorf("appending measurement sample to TSDB: %w", err)
				}

				lblsTarget := labels.FromMap(mergeMap(lblsBase, map[string]string{"__name__": "k8s_slo_target"}))
				if err := appendAt(lblsTarget, e.TargetVal); err != nil {
					return fmt.Errorf("appending target sample to TSDB: %w", err)
				}
			}

			return app.Commit()
		})
	if err != nil {
		return err
	}

	fmt.Printf("Native Thanos TSDB block created & injected successfully: ULID %s in %s\n", blockULID, tsdbDir)
	return nil
}

func findLocalCL2JSONFiles(runDir string) ([]string, error) {
	searchDir := filepath.Join(runDir, "artifacts")
	if _, err := os.Stat(searchDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("canonical artifacts directory does not exist: %s", searchDir)
	}

	var jsonFiles []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		filename := info.Name()
		if strings.HasSuffix(filename, ".json") && (strings.HasPrefix(filename, "APIResponsivenessPrometheus_") ||
			strings.HasPrefix(filename, "PodStartupLatency_") ||
			strings.HasPrefix(filename, "APIAvailability_") ||
			strings.HasPrefix(filename, "SchedulingThroughputPrometheus_") ||
			strings.HasPrefix(filename, "DnsLookupLatency_")) {
			jsonFiles = append(jsonFiles, path)
		}
		return nil
	})
	return jsonFiles, err
}

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

func categorizeEntry(labels map[string]string) string {
	verb := strings.ToUpper(labels["Verb"])
	scope := strings.ToLower(labels["Scope"])
	resource := strings.ToLower(labels["Resource"])
	subresource := strings.ToLower(labels["Subresource"])

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

func extractItems(file *MetricFile) []MetricItem {
	if file == nil {
		return nil
	}
	if len(file.DataItems) > 0 {
		return file.DataItems
	}
	return file.Data
}

type APIAvailabilityFile struct {
	ClusterMetrics struct {
		AvailabilityPercentage float64 `json:"availabilityPercentage"`
	} `json:"clusterMetrics"`
}

type SchedulingThroughputFile struct {
	Max float64 `json:"max"`
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

var sloTargetsMS = map[string]float64{
	"API Call Latency (Mutating) [s]":       1000.0,  // 1.0s
	"API Call Latency (Read-Only) [s]":      1000.0,  // 1.0s
	"API Call Latency (List Cluster) [s]":   30000.0, // 30.0s
	"API Call Latency (List Namespace) [s]": 5000.0,  // 5.0s
	"Pod Startup Latency [s]":               5000.0,  // 5.0s
	"DNS Lookup Latency [s]":                5000.0,  // 5.0s
}
