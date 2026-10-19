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
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/tsdb"
	"github.com/prometheus/tsdb/chunkenc"
	"github.com/prometheus/tsdb/chunks"
	"github.com/prometheus/tsdb/labels"
	"k8s.io/klog"
)

const (
	snapshotBlockSource = "perflens-snapshot-ingest"
	// snapshotAnchorTimeMillis is 2000-01-01T00:00:00Z in Unix milliseconds.
	// Every ingested Prometheus snapshot block is re-based onto this common origin
	// anchored at ClusterLoader2's first step so multiple runs align at t=0.
	snapshotAnchorTimeMillis int64 = 946684800000
	maxCL2LogScanBytes             = 1 << 20
	maxSnapshotRunSpanMS     int64 = 12 * 60 * 60 * 1000
	maxSnapshotEntryBytes    int64 = 2 << 30
	maxSnapshotTotalBytes    int64 = 8 << 30
	maxSnapshotEntries             = 10000
)

type cl2StepTime struct {
	mm     string
	dd     string
	hhmmss string
}

// IngestSnapshotArchive streams a Prometheus snapshot tar/tar.gz archive, re-bases TSDB block timestamps
// onto 2000-01-01T00:00:00Z anchored at the first ClusterLoader2 step, and installs the block(s) into tsdbDir.
func (ti *ThanosIngester) IngestSnapshotArchive(ctx context.Context, jobKey string, buildNumber int, archiveReader io.Reader, step cl2StepTime) error {
	if err := os.MkdirAll(ti.tsdbDir, 0755); err != nil {
		return fmt.Errorf("creating tsdb dir %s: %w", ti.tsdbDir, err)
	}
	tsdbDirAbs, err := filepath.Abs(ti.tsdbDir)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(tsdbDirAbs, ".staging-snapshot-*")
	if err != nil {
		return fmt.Errorf("creating snapshot staging dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	extractDir := filepath.Join(workDir, "extracted")
	if err := extractTarArchive(archiveReader, extractDir); err != nil {
		return fmt.Errorf("extracting snapshot archive: %w", err)
	}

	blockDirs, windowStartMS, err := findExtractedSnapshotBlocks(extractDir)
	if err != nil {
		return err
	}
	if len(blockDirs) == 0 {
		return fmt.Errorf("no valid TSDB blocks found in snapshot archive for %s build %d", jobKey, buildNumber)
	}

	anchorMS := windowStartMS
	anchorSource := "start of scraped data"
	if step.mm != "" {
		if ms, ok := klogTimeToMillis(step.mm, step.dd, step.hhmmss, windowStartMS); ok {
			anchorMS = ms
			anchorSource = "clusterloader2 first step"
		}
	}
	offsetMS := snapshotAnchorTimeMillis - anchorMS
	runName := fmt.Sprintf("run-%d", buildNumber)
	buildStr := strconv.Itoa(buildNumber)

	outStagingDir := filepath.Join(workDir, "out")
	if err := os.MkdirAll(outStagingDir, 0755); err != nil {
		return err
	}

	compactor, err := tsdb.NewLeveledCompactor(ctx, nil, ti.logger, []int64{defaultHeadChunkRangeMS}, nil)
	if err != nil {
		return fmt.Errorf("creating TSDB compactor: %w", err)
	}

	var stagedBlockDirs []string
	for _, srcBlockDir := range blockDirs {
		stagedDir, err := ti.rewriteSnapshotBlock(compactor, srcBlockDir, outStagingDir, jobKey, runName, buildStr, windowStartMS, offsetMS)
		if err != nil {
			return err
		}
		if stagedDir != "" {
			stagedBlockDirs = append(stagedBlockDirs, stagedDir)
		}
	}

	if len(stagedBlockDirs) == 0 {
		return fmt.Errorf("snapshot archive for %s build %d produced 0 non-empty blocks", jobKey, buildNumber)
	}

	jobLock := ti.jobMutex(jobKey)
	jobLock.Lock()
	defer jobLock.Unlock()

	if err := ti.removeSnapshotRunBlocks(tsdbDirAbs, jobKey, runName); err != nil {
		return err
	}

	var installedDirs []string
	for _, stagedDir := range stagedBlockDirs {
		finalDir := filepath.Join(tsdbDirAbs, filepath.Base(stagedDir))
		if err := os.Rename(stagedDir, finalDir); err != nil {
			for _, dir := range installedDirs {
				_ = os.RemoveAll(dir)
			}
			return fmt.Errorf("moving snapshot block to %s: %w", finalDir, err)
		}
		installedDirs = append(installedDirs, finalDir)
	}

	ti.markSnapshotIngested(jobKey, runName)

	klog.Infof("Ingested %d snapshot block(s) for %s %s (anchor %s [%s] -> 2000-01-01T00:00:00Z)",
		len(stagedBlockDirs), jobKey, runName, time.UnixMilli(anchorMS).UTC().Format(time.RFC3339), anchorSource)
	return nil
}

// PruneJobSnapshots deletes snapshot blocks on disk for jobKey that are older than the newest maxSnapshots builds.
func (ti *ThanosIngester) PruneJobSnapshots(jobKey string, maxSnapshots int) error {
	if maxSnapshots <= 0 {
		return nil
	}
	jobLock := ti.jobMutex(jobKey)
	jobLock.Lock()
	defer jobLock.Unlock()

	tsdbDirAbs, err := filepath.Abs(ti.tsdbDir)
	if err != nil {
		return err
	}
	files, err := os.ReadDir(tsdbDirAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type snapBlock struct {
		dir   string
		build string
		run   string
	}
	var blocks []snapBlock
	buildSet := make(map[string]bool)

	for _, file := range files {
		if !file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}
		blockDir := filepath.Join(tsdbDirAbs, file.Name())
		meta, err := readBlockMeta(blockDir)
		if err != nil {
			klog.Warningf("Skipping unreadable block metadata in %s: %v", blockDir, err)
			continue
		}
		if meta.Thanos.Source != snapshotBlockSource || meta.Thanos.Labels["perflens_job"] != jobKey {
			continue
		}
		runName := meta.Thanos.Labels["run"]
		buildStr := strings.TrimPrefix(runName, "run-")
		if buildStr == "" {
			continue
		}
		buildSet[buildStr] = true
		blocks = append(blocks, snapBlock{
			dir:   blockDir,
			build: buildStr,
			run:   runName,
		})
	}

	if len(buildSet) <= maxSnapshots {
		return nil
	}

	expiredSet := findExpiredBuilds(buildSet, maxSnapshots)
	prunedRuns := make(map[string]bool)
	for _, b := range blocks {
		if expiredSet[b.build] {
			if err := os.RemoveAll(b.dir); err != nil {
				return fmt.Errorf("removing expired snapshot block %s: %w", b.dir, err)
			}
			prunedRuns[b.run] = true
			klog.Infof("Pruned expired snapshot block %s for %s (%s)", filepath.Base(b.dir), jobKey, b.run)
		}
	}
	ti.unmarkSnapshotsIngested(jobKey, prunedRuns)
	return nil
}

// HasSnapshot reports whether a Prometheus snapshot for the given job prefix and build number is already ingested.
func (ti *ThanosIngester) HasSnapshot(jobKey string, buildNumber int) bool {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return ti.ingestedSnapshots[snapshotKey(jobKey, fmt.Sprintf("run-%d", buildNumber))]
}

// HasNoSnapshot reports whether a build was already checked and found to have no Prometheus snapshot artifact.
func (ti *ThanosIngester) HasNoSnapshot(jobKey string, buildNumber int) bool {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	return ti.noSnapshotBuilds[snapshotKey(jobKey, fmt.Sprintf("run-%d", buildNumber))]
}

// MarkNoSnapshot records that a build has no Prometheus snapshot artifact.
func (ti *ThanosIngester) MarkNoSnapshot(jobKey string, buildNumber int) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	ti.noSnapshotBuilds[snapshotKey(jobKey, fmt.Sprintf("run-%d", buildNumber))] = true
}

func (g *Downloader) syncJobSnapshots(ctx context.Context, job string, tests Tests, buildNumbers []int, buildsToFetch int, cache *artifactsCache) {
	if !g.shouldIngestSnapshots(tests.Prefix) || g.Options.SnapshotBuildsCount <= 0 {
		return
	}
	retained := 0
	for index := 0; index < buildsToFetch && index < len(buildNumbers) && retained < g.Options.SnapshotBuildsCount; index++ {
		buildNumber := buildNumbers[index]
		if g.thanosIngester.HasSnapshot(tests.Prefix, buildNumber) {
			retained++
			continue
		}
		if g.thanosIngester.HasNoSnapshot(tests.Prefix, buildNumber) {
			continue
		}
		ingested, err := g.ingestBuildSnapshot(ctx, job, tests, buildNumber, cache)
		if err != nil {
			klog.Errorf("Error ingesting Prometheus snapshot for %s build %d: %v", job, buildNumber, err)
			continue
		}
		if ingested {
			retained++
		}
	}
	if err := g.thanosIngester.PruneJobSnapshots(tests.Prefix, g.Options.SnapshotBuildsCount); err != nil {
		klog.Errorf("Error pruning Prometheus snapshots for %s: %v", tests.Prefix, err)
	}
}

func (g *Downloader) ingestBuildSnapshot(ctx context.Context, job string, tests Tests, buildNumber int, cache *artifactsCache) (bool, error) {
	searchPrefix := g.artifactName(tests, "prometheus_snapshot.tar")
	matches, err := cache.getMatchingFiles(job, buildNumber, searchPrefix)
	if err != nil {
		return false, fmt.Errorf("listing snapshot artifacts: %w", err)
	}
	if len(matches) == 0 {
		if g.isBuildFinished(job, buildNumber) {
			g.thanosIngester.MarkNoSnapshot(tests.Prefix, buildNumber)
		}
		return false, nil
	}

	var chosenFile string
	for _, m := range matches {
		base := filepath.Base(m)
		if base == "prometheus_snapshot.tar.gz" {
			chosenFile = base
			break
		}
		if base == "prometheus_snapshot.tar" {
			chosenFile = base
		}
	}
	if chosenFile == "" {
		if g.isBuildFinished(job, buildNumber) {
			g.thanosIngester.MarkNoSnapshot(tests.Prefix, buildNumber)
		}
		return false, nil
	}

	var step cl2StepTime
	for _, logPath := range []string{"build-log.txt", g.artifactName(tests, "build-log.txt")} {
		rc, err := g.MetricsBkt.OpenFile(job, buildNumber, logPath)
		if err != nil {
			continue
		}
		parsedStep, ok := firstStepFromLogReader(rc)
		_ = rc.Close()
		if ok {
			step = parsedStep
			break
		}
	}

	relArchivePath := g.artifactName(tests, chosenFile)
	klog.Infof("Downloading and ingesting Prometheus snapshot %s for %s (%s) build %d...", relArchivePath, tests.Prefix, job, buildNumber)
	rc, err := g.MetricsBkt.OpenFile(job, buildNumber, relArchivePath)
	if err != nil {
		return false, fmt.Errorf("opening snapshot archive %s: %w", relArchivePath, err)
	}
	defer rc.Close()

	if err := g.thanosIngester.IngestSnapshotArchive(ctx, tests.Prefix, buildNumber, rc, step); err != nil {
		return false, err
	}
	return true, nil
}

func (g *Downloader) isBuildFinished(job string, buildNumber int) bool {
	_, err := g.MetricsBkt.ReadFile(job, buildNumber, "finished.json")
	return err == nil
}

func (ti *ThanosIngester) rewriteSnapshotBlock(compactor tsdb.Compactor, srcBlockDir, outStagingDir, jobKey, runName, buildStr string, windowStartMS, offsetMS int64) (string, error) {
	srcBlock, err := tsdb.OpenBlock(ti.logger, srcBlockDir, nil)
	if err != nil {
		return "", fmt.Errorf("opening snapshot block %s: %w", filepath.Base(srcBlockDir), err)
	}
	defer srcBlock.Close()

	if srcBlock.MinTime()-windowStartMS > maxSnapshotRunSpanMS {
		klog.Infof("Skipping post-run snapshot block %s (minTime %d > windowStart %d + 12h)", filepath.Base(srcBlockDir), srcBlock.MinTime(), windowStartMS)
		return "", nil
	}

	shiftedMin := srcBlock.MinTime() + offsetMS
	shiftedMax := srcBlock.MaxTime() + offsetMS
	reader := &shiftedBlockReader{
		Block:    srcBlock,
		offsetMS: offsetMS,
		minTime:  shiftedMin,
		maxTime:  shiftedMax,
	}

	uid, err := compactor.Write(outStagingDir, reader, shiftedMin, shiftedMax, nil)
	if err != nil {
		return "", fmt.Errorf("rewriting shifted snapshot block %s: %w", filepath.Base(srcBlockDir), err)
	}
	if uid == ([16]byte{}) {
		return "", nil
	}

	stagedDir := filepath.Join(outStagingDir, uid.String())
	if err := writeBlockMeta(stagedDir, thanosMeta{
		Labels: map[string]string{
			"perflens_job": jobKey,
			"run":          runName,
		},
		Builds: []string{buildStr},
		Source: snapshotBlockSource,
	}); err != nil {
		return "", err
	}
	return stagedDir, nil
}

func (ti *ThanosIngester) removeSnapshotRunBlocks(tsdbDirAbs, jobKey, runName string) error {
	files, err := os.ReadDir(tsdbDirAbs)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.IsDir() || strings.HasPrefix(file.Name(), ".") {
			continue
		}
		blockDir := filepath.Join(tsdbDirAbs, file.Name())
		meta, err := readBlockMeta(blockDir)
		if err != nil {
			klog.Warningf("Skipping unreadable block metadata in %s: %v", blockDir, err)
			continue
		}
		if meta.Thanos.Source != snapshotBlockSource {
			continue
		}
		if meta.Thanos.Labels["perflens_job"] != jobKey || meta.Thanos.Labels["run"] != runName {
			continue
		}
		if err := os.RemoveAll(blockDir); err != nil {
			return err
		}
	}
	return nil
}

func (ti *ThanosIngester) markSnapshotIngested(jobKey, runName string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	ti.ingestedSnapshots[snapshotKey(jobKey, runName)] = true
}

func (ti *ThanosIngester) unmarkSnapshotsIngested(jobKey string, expiredRuns map[string]bool) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	for runName := range expiredRuns {
		delete(ti.ingestedSnapshots, snapshotKey(jobKey, runName))
	}
}

func snapshotKey(jobKey, runName string) string {
	return fmt.Sprintf("%s/%s", jobKey, runName)
}

type shiftedBlockReader struct {
	*tsdb.Block
	offsetMS int64
	minTime  int64
	maxTime  int64
}

func (r *shiftedBlockReader) MinTime() int64 {
	return r.minTime
}

func (r *shiftedBlockReader) MaxTime() int64 {
	return r.maxTime
}

func (r *shiftedBlockReader) Meta() tsdb.BlockMeta {
	m := r.Block.Meta()
	m.MinTime = r.minTime
	m.MaxTime = r.maxTime
	return m
}

func (r *shiftedBlockReader) Index() (tsdb.IndexReader, error) {
	ir, err := r.Block.Index()
	if err != nil {
		return nil, err
	}
	return &shiftedIndexReader{
		IndexReader: ir,
		offsetMS:    r.offsetMS,
		minTime:     r.minTime,
		maxTime:     r.maxTime,
	}, nil
}

func (r *shiftedBlockReader) Chunks() (tsdb.ChunkReader, error) {
	cr, err := r.Block.Chunks()
	if err != nil {
		return nil, err
	}
	return &shiftedChunkReader{
		ChunkReader: cr,
		offsetMS:    r.offsetMS,
	}, nil
}

type shiftedIndexReader struct {
	tsdb.IndexReader
	offsetMS int64
	minTime  int64
	maxTime  int64
}

func (r *shiftedIndexReader) Series(ref uint64, lset *labels.Labels, chks *[]chunks.Meta) error {
	if err := r.IndexReader.Series(ref, lset, chks); err != nil {
		return err
	}
	for i := range *chks {
		(*chks)[i].MinTime += r.offsetMS
		(*chks)[i].MaxTime += r.offsetMS
		if (*chks)[i].MinTime < r.minTime {
			(*chks)[i].MinTime = r.minTime
		}
		if (*chks)[i].MaxTime > r.maxTime {
			(*chks)[i].MaxTime = r.maxTime
		}
	}
	return nil
}

type shiftedChunkReader struct {
	tsdb.ChunkReader
	offsetMS int64
}

func (r *shiftedChunkReader) Chunk(ref uint64) (chunkenc.Chunk, error) {
	c, err := r.ChunkReader.Chunk(ref)
	if err != nil {
		return nil, err
	}
	return shiftXORChunk(c, r.offsetMS)
}

func shiftXORChunk(c chunkenc.Chunk, offsetMS int64) (chunkenc.Chunk, error) {
	if offsetMS == 0 || c.Encoding() != chunkenc.EncXOR {
		return c, nil
	}
	raw := c.Bytes()
	if len(raw) <= 2 || binary.BigEndian.Uint16(raw[:2]) == 0 {
		return c, nil
	}
	t0, n := binary.Varint(raw[2:])
	if n <= 0 {
		return nil, fmt.Errorf("invalid varint timestamp in XOR chunk")
	}
	var varintBuf [binary.MaxVarintLen64]byte
	m := binary.PutVarint(varintBuf[:], t0+offsetMS)
	out := make([]byte, 2+m+len(raw)-(2+n))
	copy(out[:2], raw[:2])
	copy(out[2:2+m], varintBuf[:m])
	copy(out[2+m:], raw[2+n:])
	return chunkenc.FromData(chunkenc.EncXOR, out)
}

func extractTarArchive(r io.Reader, destDir string) error {
	destRoot, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destRoot, 0755); err != nil {
		return err
	}

	br := bufio.NewReaderSize(r, 64*1024)
	magic, peekErr := br.Peek(2)
	isGzipStream := peekErr == nil && magic[0] == 0x1f && magic[1] == 0x8b

	var tarInput io.Reader = br
	if isGzipStream {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("opening gzip stream: %w", err)
		}
		defer gz.Close()
		tarInput = gz
	}

	tr := tar.NewReader(tarInput)
	var totalBytes int64
	var totalEntries int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}
		totalEntries++
		if totalEntries > maxSnapshotEntries {
			return fmt.Errorf("snapshot archive exceeds maximum entry count (%d)", maxSnapshotEntries)
		}

		target := filepath.Join(destRoot, filepath.Clean(hdr.Name))
		if target != destRoot && !strings.HasPrefix(target, destRoot+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if target == destRoot {
				return fmt.Errorf("tar regular file entry %q resolves to destination root", hdr.Name)
			}
			if hdr.Size > maxSnapshotEntryBytes {
				return fmt.Errorf("tar entry %q size %d exceeds limit %d", hdr.Name, hdr.Size, maxSnapshotEntryBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(f, io.LimitReader(tr, maxSnapshotEntryBytes+1))
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written > maxSnapshotEntryBytes {
				return fmt.Errorf("tar entry %q exceeded maximum size %d", hdr.Name, maxSnapshotEntryBytes)
			}
			totalBytes += written
			if totalBytes > maxSnapshotTotalBytes {
				return fmt.Errorf("snapshot archive exceeded maximum total extracted size (%d bytes)", maxSnapshotTotalBytes)
			}
		}
	}
	return nil
}

func findExtractedSnapshotBlocks(rootDir string) ([]string, int64, error) {
	type blockCandidate struct {
		dir     string
		minTime int64
	}
	var candidates []blockCandidate
	var windowStart int64
	found := false

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "index")); err != nil {
			return nil
		}
		metaBytes, err := os.ReadFile(filepath.Join(path, "meta.json"))
		if err != nil {
			klog.Warningf("Skipping snapshot block %s with unreadable meta.json: %v", path, err)
			return nil
		}
		var meta blockMeta
		if err := json.Unmarshal(metaBytes, &meta); err != nil || meta.MaxTime <= meta.MinTime {
			klog.Warningf("Skipping snapshot block %s with invalid meta.json: %v", path, err)
			return nil
		}
		if !found || meta.MinTime < windowStart {
			windowStart = meta.MinTime
			found = true
		}
		candidates = append(candidates, blockCandidate{dir: path, minTime: meta.MinTime})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].minTime < candidates[j].minTime
	})
	dirs := make([]string, len(candidates))
	for i, c := range candidates {
		dirs[i] = c.dir
	}
	return dirs, windowStart, nil
}

var cl2StepStartRe = regexp.MustCompile(`[IWEF](\d{2})(\d{2})\s+(\d{2}:\d{2}:\d{2})(?:\.\d+)?\s.*Step\s+"`)

func firstStepFromLogReader(r io.Reader) (cl2StepTime, bool) {
	scanner := bufio.NewScanner(io.LimitReader(r, maxCL2LogScanBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), maxCL2LogScanBytes)
	for scanner.Scan() {
		m := cl2StepStartRe.FindStringSubmatch(scanner.Text())
		if m != nil {
			return cl2StepTime{mm: m[1], dd: m[2], hhmmss: m[3]}, true
		}
	}
	if err := scanner.Err(); err != nil {
		klog.Warningf("Error scanning build-log.txt for first ClusterLoader2 step: %v", err)
	}
	return cl2StepTime{}, false
}

func klogTimeToMillis(mm, dd, hhmmss string, hintMS int64) (int64, bool) {
	hint := time.UnixMilli(hintMS).UTC()
	var best int64
	found := false
	for _, year := range []int{hint.Year() - 1, hint.Year(), hint.Year() + 1} {
		stamp := fmt.Sprintf("%04d-%s-%sT%sZ", year, mm, dd, hhmmss)
		t, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		ms := t.UnixMilli()
		if !found || absDiffInt64(ms, hintMS) < absDiffInt64(best, hintMS) {
			best = ms
			found = true
		}
	}
	return best, found
}

func absDiffInt64(a, b int64) int64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
