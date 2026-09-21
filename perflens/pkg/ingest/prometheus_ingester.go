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

	"github.com/thanos-io/thanos/pkg/block/metadata"
)

// PrometheusIngester processes downloaded Prometheus TSDB snapshot blocks and injects Thanos metadata.
type PrometheusIngester struct{}

// NewPrometheusIngester creates a new instance of PrometheusIngester.
func NewPrometheusIngester() *PrometheusIngester {
	return &PrometheusIngester{}
}

// Ingest ingests local Prometheus snapshot TSDB blocks into tsdbDir.
func (p *PrometheusIngester) Ingest(ctx context.Context, runDir string, runName string, tsdbDir string) error {
	localSnapshotDir := filepath.Join(runDir, "artifacts", "prometheus", "snapshots")

	if _, err := os.Stat(localSnapshotDir); os.IsNotExist(err) {
		return fmt.Errorf("no Prometheus snapshot TSDB blocks found at canonical path %s", localSnapshotDir)
	}

	foundBlocks := 0
	err := filepath.Walk(localSnapshotDir, func(path string, info os.FileInfo, err error) error {
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
				if err := p.injectThanosRunLabel(targetDir, runName); err != nil {
					fmt.Printf("Warning: Failed injecting Thanos metadata into %s: %v\n", blockULID, err)
				}
				foundBlocks++
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error reading TSDB snapshot blocks in %s: %w", localSnapshotDir, err)
	}

	if foundBlocks == 0 {
		return fmt.Errorf("no valid Prometheus snapshot TSDB blocks found in %s", localSnapshotDir)
	}

	fmt.Printf("Successfully ingested %d local Prometheus snapshot TSDB blocks for %s\n", foundBlocks, runName)
	return nil
}

func (p *PrometheusIngester) injectThanosRunLabel(blockDir string, runName string) error {
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
