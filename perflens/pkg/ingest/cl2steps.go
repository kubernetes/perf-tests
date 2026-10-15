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
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// cl2StepStartRe matches the step marker ClusterLoader2 logs at klog -v=2, from
// clusterloader2/pkg/test/simple_test_executor.go:
//
//	I0726 16:23:51.123456      12 simple_test_executor.go:162] Step "foo" started
//
// klog omits the year, so only MMDD and the time of day are captured.
var cl2StepStartRe = regexp.MustCompile(`[IWEF](\d{2})(\d{2})\s+(\d{2}:\d{2}:\d{2})(?:\.\d+)?\s.*Step\s+"`)

// maxCL2LogScanBytes bounds a single log line. Prow build logs interleave every
// component's output, and a stray binary blob would otherwise abort the scan
// with ErrTooLong before the first step marker is reached.
const maxCL2LogScanBytes = 1 << 20

// cl2TestStart returns the wall clock time of ClusterLoader2's first step, in
// Unix millis. That is when the test proper begins, which is later, and by a
// run dependent amount, than when Prometheus started scraping. Anchoring on it
// is what makes two runs line up at the same event rather than at whatever
// their bring-up happened to cost.
//
// hintMS is any timestamp known to fall inside the run, used to recover the
// year that klog leaves out.
func cl2TestStart(runDir string, hintMS int64) (int64, bool) {
	var best int64
	found := false

	for _, path := range findCL2LogFiles(runDir) {
		ms, ok := firstStepStart(path, hintMS)
		if !ok {
			continue
		}
		if !found || ms < best {
			best = ms
			found = true
		}
	}
	return best, found
}

// findCL2LogFiles lists the files that may carry ClusterLoader2's own output.
// On Prow the framework logs to the job level build-log.txt, which sits beside
// artifacts/ rather than inside it, so both locations are searched.
func findCL2LogFiles(runDir string) []string {
	var files []string

	for _, name := range []string{
		filepath.Join(runDir, "build-log.txt"),
		filepath.Join(runDir, "artifacts", "build-log.txt"),
	} {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			files = append(files, name)
		}
	}

	_ = filepath.Walk(filepath.Join(runDir, "artifacts"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasPrefix(name, "clusterloader") && strings.Contains(name, ".log") {
			files = append(files, path)
		}
		return nil
	})

	return files
}

// firstStepStart scans one log file for the earliest step marker.
func firstStepStart(path string, hintMS int64) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxCL2LogScanBytes)
	for scanner.Scan() {
		m := cl2StepStartRe.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		if ms, ok := klogTimeToMillis(m[1], m[2], m[3], hintMS); ok {
			return ms, true
		}
	}
	return 0, false
}

// klogTimeToMillis resolves a klog MMDD plus time of day to Unix millis. klog
// prints no year, so the candidate nearest hintMS wins. Trying the neighbouring
// years too is what keeps a run that straddles New Year from landing 12 months
// away from its own metrics.
func klogTimeToMillis(mm, dd, hhmmss string, hintMS int64) (int64, bool) {
	hint := time.UnixMilli(hintMS).UTC()

	var best int64
	found := false
	for _, year := range []int{hint.Year() - 1, hint.Year(), hint.Year() + 1} {
		stamp := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006") +
			"-" + mm + "-" + dd + "T" + hhmmss + "Z"
		t, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		ms := t.UnixMilli()
		if !found || absInt64(ms-hintMS) < absInt64(best-hintMS) {
			best = ms
			found = true
		}
	}
	return best, found
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
