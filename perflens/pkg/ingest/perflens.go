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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Run executes the metric ingestion pipeline for a given build ID, mode, and artifacts root directory.
func Run(buildID string, mode string, artifactsDir string, normalizeTime bool) error {
	rawBuildID := strings.TrimPrefix(buildID, "run-")
	runName := "run-" + rawBuildID

	// Resolve canonical run directory and target output directories
	runDir := buildID
	if info, err := os.Stat(runDir); err != nil || !info.IsDir() {
		runDir = filepath.Join(artifactsDir, "runs", rawBuildID)
	}

	tsdbDir := filepath.Join(artifactsDir, "prometheus")
	omDir := filepath.Join(artifactsDir, "openmetrics")

	ctx := context.Background()
	modeLower := strings.ToLower(mode)

	// One Timebase for the whole run. Every ingester shifts by this same offset,
	// otherwise the run's blocks would desync against each other.
	timebase := NewTimebase(runDir, runName, normalizeTime)
	fmt.Println(timebase.Describe())
	if normalizeTime && !timebase.Known {
		fmt.Fprintf(os.Stderr, "Warning: could not determine the wall clock window of %s, ingesting without normalization\n", runName)
	}

	if err := writeRunTimebaseBlock(ctx, tsdbDir, timebase); err != nil {
		fmt.Fprintf(os.Stderr, "Warning/Info: %v\n", err)
	}

	if modeLower == "slo" || modeLower == "all" {
		sloIngester := NewSLOIngester()
		if err := sloIngester.Ingest(ctx, runDir, runName, tsdbDir, omDir, timebase); err != nil {
			fmt.Fprintf(os.Stderr, "Error ingesting SLO metrics for build %s: %v\n", buildID, err)
			if modeLower != "all" {
				return err
			}
		}
	}

	if modeLower == "prometheus-metrics" || modeLower == "prometheus" || modeLower == "all" {
		promIngester := NewPrometheusIngester()
		if err := promIngester.Ingest(ctx, runDir, runName, tsdbDir, timebase); err != nil {
			fmt.Fprintf(os.Stderr, "Warning/Info: Prometheus snapshot ingestion for build %s: %v\n", buildID, err)
		}
	}

	if modeLower == "log-metrics" || modeLower == "logs" || modeLower == "all" {
		logIngester := NewLogIngester()
		if err := logIngester.Ingest(ctx, runDir, runName, tsdbDir, omDir, timebase); err != nil {
			fmt.Fprintf(os.Stderr, "Warning/Info: Log trace ingestion for build %s: %v\n", buildID, err)
		}
	}

	return nil
}
