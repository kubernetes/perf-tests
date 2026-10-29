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
	"path/filepath"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// commitEverySamples bounds how much an in-flight appender transaction holds in
// memory while a large snapshot block is rewritten. Commits only happen on a
// series boundary, so this is a floor rather than an exact batch size. Tests
// lower it to exercise the multi-transaction path.
var commitEverySamples = 2_000_000

// PrometheusIngester processes downloaded Prometheus TSDB snapshot blocks and injects Thanos metadata.
type PrometheusIngester struct{}

// NewPrometheusIngester creates a new instance of PrometheusIngester.
func NewPrometheusIngester() *PrometheusIngester {
	return &PrometheusIngester{}
}

// Ingest ingests local Prometheus snapshot TSDB blocks into tsdbDir, re-basing
// every sample onto the run's Timebase.
func (p *PrometheusIngester) Ingest(ctx context.Context, runDir string, runName string, tsdbDir string, tb *Timebase) error {
	blockDirs := findSnapshotBlockDirs(runDir)
	if len(blockDirs) == 0 {
		return fmt.Errorf("no Prometheus snapshot TSDB blocks found under %s",
			filepath.Join(runDir, "artifacts", "prometheus", "snapshots"))
	}

	// Sweep once for the whole run, not per block, because a run can produce
	// several snapshot blocks that all belong to the same ingest.
	if removed, err := removeRunBlocks(tsdbDir, runName, sourceSnapshot); err != nil {
		return err
	} else if removed > 0 {
		fmt.Printf("Replacing %d previously ingested snapshot block(s) of %s\n", removed, runName)
	}

	ingested := 0
	for _, blockDir := range blockDirs {
		if err := p.ingestBlock(ctx, blockDir, runName, tsdbDir, tb); err != nil {
			fmt.Printf("Warning: Failed ingesting snapshot block %s: %v\n", filepath.Base(blockDir), err)
			continue
		}
		ingested++
	}

	if ingested == 0 {
		return fmt.Errorf("no valid Prometheus snapshot TSDB blocks ingested from %s", runDir)
	}

	fmt.Printf("Successfully ingested %d local Prometheus snapshot TSDB blocks for %s\n", ingested, runName)
	return nil
}

// ingestBlock rewrites one snapshot block into tsdbDir with shifted timestamps.
func (p *PrometheusIngester) ingestBlock(ctx context.Context, srcDir string, runName string, tsdbDir string, tb *Timebase) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	blk, err := tsdb.OpenBlock(logger, srcDir, chunkenc.NewPool(), nil)
	if err != nil {
		return fmt.Errorf("opening block %s: %w", srcDir, err)
	}
	defer blk.Close()

	meta := blk.Meta()
	fmt.Printf("Rewriting snapshot block %s (%d series, %d samples, %s to %s)...\n",
		meta.ULID, meta.Stats.NumSeries, meta.Stats.NumSamples,
		time.UnixMilli(meta.MinTime).UTC().Format(time.RFC3339),
		time.UnixMilli(meta.MaxTime).UTC().Format(time.RFC3339))

	blockSize := blockSizeForSpan(tb.Shift(meta.MinTime), tb.Shift(meta.MaxTime))
	written, err := writeThanosBlock(ctx, tsdbDir, runName, sourceSnapshot, blockSize,
		func(bw *tsdb.BlockWriter) error {
			return copyShiftedSamples(ctx, blk, bw, tb)
		})
	if err != nil {
		return err
	}

	fmt.Printf("  wrote block %s starting at %s\n", written,
		time.UnixMilli(tb.Shift(meta.MinTime)).UTC().Format(time.RFC3339))
	return nil
}

// copyShiftedSamples streams every sample of src into bw with its timestamp
// shifted by the run's offset.
func copyShiftedSamples(ctx context.Context, src *tsdb.Block, bw *tsdb.BlockWriter, tb *Timebase) error {
	meta := src.Meta()
	q, err := tsdb.NewBlockQuerier(src, meta.MinTime, meta.MaxTime)
	if err != nil {
		return fmt.Errorf("querying block %s: %w", meta.ULID, err)
	}
	defer q.Close()

	named := labels.MustNewMatcher(labels.MatchRegexp, labels.MetricName, ".+")
	series := q.Select(ctx, false, nil, named)

	app := bw.Appender(ctx)
	pending := 0
	var it chunkenc.Iterator

	for series.Next() {
		s := series.At()
		lbls := s.Labels()
		var ref storage.SeriesRef

		it = s.Iterator(it)
		for valType := it.Next(); valType != chunkenc.ValNone; valType = it.Next() {
			var appendErr error
			switch valType {
			case chunkenc.ValFloat:
				ts, v := it.At()
				ref, appendErr = app.Append(ref, lbls, tb.Shift(ts), v)
			case chunkenc.ValHistogram:
				ts, h := it.AtHistogram(nil)
				ref, appendErr = app.AppendHistogram(ref, lbls, tb.Shift(ts), h, nil)
			case chunkenc.ValFloatHistogram:
				ts, fh := it.AtFloatHistogram(nil)
				ref, appendErr = app.AppendHistogram(ref, lbls, tb.Shift(ts), nil, fh)
			default:
				continue
			}
			if appendErr != nil {
				return fmt.Errorf("appending sample for %s: %w", lbls.String(), appendErr)
			}
			pending++
		}
		if err := it.Err(); err != nil {
			return fmt.Errorf("iterating samples of %s: %w", lbls.String(), err)
		}

		if pending >= commitEverySamples {
			if err := app.Commit(); err != nil {
				return fmt.Errorf("committing TSDB appender: %w", err)
			}
			app = bw.Appender(ctx)
			pending = 0
		}
	}
	if err := series.Err(); err != nil {
		return fmt.Errorf("reading series of block %s: %w", meta.ULID, err)
	}

	if err := app.Commit(); err != nil {
		return fmt.Errorf("committing TSDB appender: %w", err)
	}
	return nil
}
