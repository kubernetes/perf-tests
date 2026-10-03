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
	"regexp"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

// AnchorTime is the common origin that every ingested run is re-based to. Scale
// runs are bounded experiments that happen days or weeks apart, so ingest shifts
// each run onto this instant and dashboards read elapsed time since run start.
// A round year is used rather than epoch 0 because Grafana renders 1970 badly.
var AnchorTime = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// AnchorTimeMillis is AnchorTime as a Unix millisecond timestamp.
const AnchorTimeMillis int64 = 946684800000

// RunStartMetric carries the original wall clock time that a run's normalized
// origin maps back to, so absolute time stays recoverable after normalization.
// It reports the anchor, not the first sample, so it always answers "what real
// instant is t=0 on this axis".
const RunStartMetric = "perflens_run_start_timestamp_seconds"

// cl2ArtifactTimeRe matches the RFC3339 stamp CL2 puts in its summary filenames,
// for example APIResponsivenessPrometheus_load_2026-08-05T19:44:02Z.json.
var cl2ArtifactTimeRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z)`)

// Timebase is a run's mapping from original wall clock to normalized time. One
// Timebase is computed per run and shared by every ingester, so the snapshot,
// SLO and log blocks of a run all shift by the same amount and stay aligned
// with each other.
type Timebase struct {
	RunName string
	// Enabled reports whether timestamps are re-based. When false Shift is the
	// identity and samples keep their original wall clock.
	Enabled bool
	// Known reports whether the run's wall clock window could be determined.
	Known bool
	// StartMS and EndMS are the run's original wall clock window in Unix millis.
	// This is the extent of the data, which begins before the test does.
	StartMS int64
	EndMS   int64
	// AnchorMS is the original wall clock instant that maps onto AnchorTime, in
	// other words what t=0 means on the normalized axis. It is the start of the
	// test rather than the start of the data, so samples scraped during bring-up
	// land before the origin.
	AnchorMS int64
	// AnchorSource names where AnchorMS came from, for the ingest log.
	AnchorSource string
	// OffsetMS is added to every sample timestamp of this run.
	OffsetMS int64
}

// NewTimebase determines a run's wall clock window and the offset that maps its
// anchor onto AnchorTime.
func NewTimebase(runDir string, runName string, normalize bool) *Timebase {
	tb := &Timebase{RunName: runName}

	start, end, ok := runWallClockWindow(runDir)
	if !ok {
		return tb
	}
	tb.Known = true
	tb.StartMS = start
	tb.EndMS = end
	tb.AnchorMS, tb.AnchorSource = runAnchor(runDir, start)

	if normalize {
		tb.Enabled = true
		tb.OffsetMS = AnchorTimeMillis - tb.AnchorMS
	}
	return tb
}

// runAnchor picks the instant that becomes t=0 for a run.
//
// ClusterLoader2's first step is the right anchor because it is the same event
// in every run. The data window start is not: it is when Prometheus began
// scraping, which trails cluster bring-up by a run dependent amount, so
// anchoring on it offsets two overlaid runs by the difference in their bring-up
// times. It stays as the fallback for runs whose CL2 log was not collected.
func runAnchor(runDir string, windowStartMS int64) (int64, string) {
	if ms, ok := cl2TestStart(runDir, windowStartMS); ok {
		return ms, "clusterloader2 first step"
	}
	return windowStartMS, "start of scraped data"
}

// Shift maps an original sample timestamp in Unix millis onto the normalized axis.
func (t *Timebase) Shift(ms int64) int64 {
	if t == nil || !t.Enabled {
		return ms
	}
	return ms + t.OffsetMS
}

// NormalizedStart is the timestamp a run's samples begin at after shifting.
func (t *Timebase) NormalizedStart() int64 {
	if t == nil || !t.Known {
		return time.Now().UTC().UnixMilli()
	}
	return t.Shift(t.StartMS)
}

// NormalizedEnd is the timestamp a run's samples end at after shifting.
func (t *Timebase) NormalizedEnd() int64 {
	if t == nil || !t.Known {
		return time.Now().UTC().UnixMilli()
	}
	return t.Shift(t.EndMS)
}

// Describe renders the mapping for the ingest log.
func (t *Timebase) Describe() string {
	if t == nil || !t.Enabled {
		return "time normalization off, samples keep their original wall clock"
	}
	return fmt.Sprintf("re-basing %s: anchor %s (%s) to %s, offset %s, data spans %s from %s before the anchor",
		t.RunName,
		time.UnixMilli(t.AnchorMS).UTC().Format(time.RFC3339),
		t.AnchorSource,
		AnchorTime.Format(time.RFC3339),
		time.Duration(t.OffsetMS)*time.Millisecond,
		time.Duration(t.EndMS-t.StartMS)*time.Millisecond,
		time.Duration(t.AnchorMS-t.StartMS)*time.Millisecond,
	)
}

// runWallClockWindow finds the run's original time window. The Prometheus
// snapshot is authoritative because it brackets the whole run. CL2 summary
// filenames are a fallback for runs ingested without a snapshot.
func runWallClockWindow(runDir string) (start int64, end int64, ok bool) {
	if start, end, ok = snapshotWindow(runDir); ok {
		return start, end, true
	}
	return artifactFilenameWindow(runDir)
}

// snapshotWindow returns the min minTime and max maxTime over every TSDB block
// in the run's Prometheus snapshot. Taking the extremes over all blocks, rather
// than per block, is what keeps a multi-block run internally aligned.
func snapshotWindow(runDir string) (int64, int64, bool) {
	var start, end int64
	found := false

	for _, blockDir := range findSnapshotBlockDirs(runDir) {
		body, err := os.ReadFile(filepath.Join(blockDir, "meta.json"))
		if err != nil {
			continue
		}
		var meta struct {
			MinTime int64 `json:"minTime"`
			MaxTime int64 `json:"maxTime"`
		}
		if err := json.Unmarshal(body, &meta); err != nil || meta.MaxTime <= meta.MinTime {
			continue
		}
		if !found || meta.MinTime < start {
			start = meta.MinTime
		}
		if !found || meta.MaxTime > end {
			end = meta.MaxTime
		}
		found = true
	}
	return start, end, found
}

// findSnapshotBlockDirs lists the TSDB block directories of a run's extracted
// Prometheus snapshot.
func findSnapshotBlockDirs(runDir string) []string {
	root := filepath.Join(runDir, "artifacts", "prometheus", "snapshots")
	if _, err := os.Stat(root); err != nil {
		return nil
	}

	var dirs []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "meta.json")); err != nil {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "index")); err != nil {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs
}

func artifactFilenameWindow(runDir string) (int64, int64, bool) {
	var start, end int64
	found := false

	_ = filepath.Walk(filepath.Join(runDir, "artifacts"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		match := cl2ArtifactTimeRe.FindString(info.Name())
		if match == "" {
			return nil
		}
		ts, err := time.Parse(time.RFC3339, match)
		if err != nil {
			return nil
		}
		ms := ts.UTC().UnixMilli()
		if !found || ms < start {
			start = ms
		}
		if !found || ms > end {
			end = ms
		}
		found = true
		return nil
	})
	return start, end, found
}

// writeRunTimebaseBlock emits RunStartMetric for the run, spanning the run's
// normalized window so the anchor's original wall clock time is readable from
// any point in the dashboard time range.
func writeRunTimebaseBlock(ctx context.Context, tsdbDir string, tb *Timebase) error {
	if tb == nil || !tb.Known {
		return fmt.Errorf("run window unknown, skipping %s", RunStartMetric)
	}

	startMS := tb.NormalizedStart()
	endMS := tb.NormalizedEnd()
	value := float64(tb.AnchorMS) / 1000.0

	lbls := labels.FromMap(map[string]string{
		"__name__": RunStartMetric,
		"run":      tb.RunName,
	})

	if _, err := removeRunBlocks(tsdbDir, tb.RunName, sourceTimebase); err != nil {
		return err
	}

	blockULID, err := writeThanosBlock(ctx, tsdbDir, tb.RunName, sourceTimebase,
		blockSizeForSpan(startMS, endMS),
		func(bw *tsdb.BlockWriter) error {
			app := bw.Appender(ctx)
			if err := appendRunSpan(app, lbls, startMS, endMS, value); err != nil {
				return err
			}
			return app.Commit()
		})
	if err != nil {
		return err
	}

	fmt.Printf("Wrote %s block %s for %s (anchor %s, %s)\n",
		RunStartMetric, blockULID, tb.RunName,
		time.UnixMilli(tb.AnchorMS).UTC().Format(time.RFC3339), tb.AnchorSource)
	return nil
}
