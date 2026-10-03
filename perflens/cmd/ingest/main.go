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
	"flag"
	"fmt"
	"os"

	"k8s.io/perf-tests/perflens/pkg/ingest"
)

func main() {
	opts := parseFlags()
	if err := ingest.Run(opts.buildID, opts.mode, opts.artifactsDir, opts.normalizeTime); err != nil {
		fmt.Fprintf(os.Stderr, "Ingestion error: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	buildID       string
	mode          string
	artifactsDir  string
	normalizeTime bool
}

func parseFlags() options {
	buildID := flag.String("build-id", "", "Prow Build ID, run identifier, or run directory path")
	mode := flag.String("mode", "all", "Ingestion mode: slo-metrics, prometheus-metrics, or all")
	artifactsDir := flag.String("artifacts-dir", "", "Root _artifacts directory")
	normalizeTime := flag.Bool("normalize-time", true, "Re-base sample timestamps so every run starts at the common anchor "+ingest.AnchorTime.Format("2006-01-02T15:04:05Z")+". Disable to keep original wall clock timestamps")
	flag.Parse()

	if *buildID == "" || *artifactsDir == "" {
		flag.Usage()
		os.Exit(1)
	}
	return options{
		buildID:       *buildID,
		mode:          *mode,
		artifactsDir:  *artifactsDir,
		normalizeTime: *normalizeTime,
	}
}
