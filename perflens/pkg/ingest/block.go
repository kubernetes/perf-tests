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
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

const twoHoursMS = int64(2 * 60 * 60 * 1000)

// runSpanStepMS is the cadence used for values that hold for a whole run, such as
// SLO verdicts and the run start marker. It has to stay well under Prometheus'
// 5 minute staleness lookback, otherwise an instant query landing between two
// samples reads the series as absent.
const runSpanStepMS = int64(60 * 1000)

// appendRunSpan writes value as a constant series across [startMS, endMS], so the
// series is readable from any instant inside the run rather than only at its edges.
func appendRunSpan(app storage.Appender, lbls labels.Labels, startMS, endMS int64, value float64) error {
	var ref storage.SeriesRef
	for ts := startMS; ts < endMS; ts += runSpanStepMS {
		var err error
		if ref, err = app.Append(ref, lbls, ts, value); err != nil {
			return err
		}
	}
	_, err := app.Append(ref, lbls, endMS, value)
	return err
}

// blockSizeForSpan returns a head chunk range wide enough to hold [minMS, maxMS].
// The TSDB head rejects any sample older than maxTime minus half the chunk range,
// so a range narrower than twice the span would silently drop the beginning of
// every series appended after the first one.
func blockSizeForSpan(minMS, maxMS int64) int64 {
	span := maxMS - minMS
	if span < 0 {
		span = 0
	}
	size := 2*span + 2
	if size < twoHoursMS {
		size = twoHoursMS
	}
	return size
}

// Thanos block sources, one per ingester. They double as the key for replacing a
// run's previous blocks, so re-running a single mode never drops another mode's work.
const (
	sourceSnapshot = "perflens-snapshot-ingest"
	sourceSLO      = "perflens-ingest"
	sourceLogs     = "perflens-log-ingest"
	sourceTimebase = "perflens-timebase"
)

// removeRunBlocks deletes the blocks in tsdbDir that this run already has from the
// given source, so a re-ingest replaces the run rather than stacking a second copy
// of it on the same axis. Blocks always get a fresh ULID rather than reusing the
// source one, because Thanos Store caches a block's index header by ULID and would
// keep serving the previous content otherwise.
func removeRunBlocks(tsdbDir string, runName string, source string) (int, error) {
	entries, err := os.ReadDir(tsdbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		blockDir := filepath.Join(tsdbDir, entry.Name())
		meta, err := metadata.ReadFromDir(blockDir)
		if err != nil || meta.Thanos.Labels["run"] != runName || string(meta.Thanos.Source) != source {
			continue
		}
		if err := os.RemoveAll(blockDir); err != nil {
			return removed, fmt.Errorf("removing stale block %s: %w", entry.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// writeThanosBlock builds a TSDB block via fill, stamps it with the run's Thanos
// external label and installs it in tsdbDir. Writing happens in a staging
// directory outside tsdbDir so Thanos Store never observes a half-built block in
// the bucket.
func writeThanosBlock(ctx context.Context, tsdbDir string, runName string, source string, blockSizeMS int64, fill func(bw *tsdb.BlockWriter) error) (ulid.ULID, error) {
	tsdbDirAbs, err := filepath.Abs(tsdbDir)
	if err != nil {
		return ulid.ULID{}, err
	}
	if err := os.MkdirAll(tsdbDirAbs, 0755); err != nil {
		return ulid.ULID{}, err
	}

	stagingRoot := filepath.Join(filepath.Dir(tsdbDirAbs), ".perflens-staging")
	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return ulid.ULID{}, err
	}
	staging, err := os.MkdirTemp(stagingRoot, "block-")
	if err != nil {
		return ulid.ULID{}, err
	}
	defer os.RemoveAll(staging)

	blockULID, err := writeTSDBBlock(ctx, staging, blockSizeMS, fill)
	if err != nil {
		return ulid.ULID{}, err
	}

	blockDir := filepath.Join(staging, blockULID.String())
	if err := stampBlockMeta(blockDir, runName, source); err != nil {
		return ulid.ULID{}, err
	}

	target := filepath.Join(tsdbDirAbs, blockULID.String())
	if err := os.RemoveAll(target); err != nil {
		return ulid.ULID{}, err
	}
	if err := os.Rename(blockDir, target); err != nil {
		return ulid.ULID{}, fmt.Errorf("installing block %s: %w", blockULID, err)
	}
	return blockULID, nil
}

func writeTSDBBlock(ctx context.Context, destDir string, blockSizeMS int64, fill func(bw *tsdb.BlockWriter) error) (ulid.ULID, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bw, err := tsdb.NewBlockWriter(logger, destDir, blockSizeMS)
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("creating TSDB BlockWriter: %w", err)
	}
	defer bw.Close()

	if err := fill(bw); err != nil {
		return ulid.ULID{}, err
	}

	blockULID, err := bw.Flush(ctx)
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("writing TSDB block: %w", err)
	}
	if blockULID == (ulid.ULID{}) {
		return ulid.ULID{}, fmt.Errorf("no block written, appender produced no series")
	}
	return blockULID, nil
}

// stampBlockMeta injects the run's Thanos external label into a freshly written block.
func stampBlockMeta(blockDir string, runName string, source string) error {
	meta, err := metadata.ReadFromDir(blockDir)
	if err != nil {
		return fmt.Errorf("reading block meta in %s: %w", blockDir, err)
	}

	meta.Thanos = metadata.Thanos{
		Labels: map[string]string{
			"run": runName,
		},
		Source: metadata.SourceType(source),
	}

	if err := meta.WriteToDir(nopLogger{}, blockDir); err != nil {
		return fmt.Errorf("writing block meta in %s: %w", blockDir, err)
	}
	return nil
}
