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
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ms(t *testing.T, rfc3339 string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("bad timestamp %q: %v", rfc3339, err)
	}
	return parsed.UnixMilli()
}

// buildLog is a trimmed excerpt of a Prow build-log.txt: some non-CL2 noise,
// then the step markers clusterloader2 emits at -v=2.
const buildLog = `I0726 16:20:11.100000       1 clusterloader.go:230] Building cluster framework
I0726 16:20:12.000000       1 imagepreload.go:96] Preloading images
I0726 16:23:51.123456      12 simple_test_executor.go:162] Step "Starting measurement for 'load'" started
I0726 16:23:51.987000      12 simple_test_executor.go:183] Step "Starting measurement for 'load'" ended
I0726 16:44:36.000000      12 simple_test_executor.go:162] Step "Collecting measurements" started
`

func writeRun(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCL2TestStartFindsFirstStep(t *testing.T) {
	hint := ms(t, "2026-07-26T16:15:00Z")

	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"job level build log", map[string]string{"build-log.txt": buildLog}},
		{"under artifacts", map[string]string{"artifacts/build-log.txt": buildLog}},
		{"named clusterloader log", map[string]string{"artifacts/clusterloader2.log": buildLog}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cl2TestStart(writeRun(t, tc.files), hint)
			if !ok {
				t.Fatal("expected to find a step marker")
			}
			if want := ms(t, "2026-07-26T16:23:51Z"); got != want {
				t.Errorf("got %d (%s), want %d (%s)",
					got, time.UnixMilli(got).UTC(), want, time.UnixMilli(want).UTC())
			}
		})
	}
}

// The earliest marker wins even when it is not in the first file walked.
func TestCL2TestStartTakesEarliestAcrossFiles(t *testing.T) {
	late := `I0726 17:00:00.000000 12 simple_test_executor.go:162] Step "later" started` + "\n"
	dir := writeRun(t, map[string]string{
		"build-log.txt":                late,
		"artifacts/clusterloader2.log": buildLog,
	})

	got, ok := cl2TestStart(dir, ms(t, "2026-07-26T16:15:00Z"))
	if !ok {
		t.Fatal("expected to find a step marker")
	}
	if want := ms(t, "2026-07-26T16:23:51Z"); got != want {
		t.Errorf("got %s, want %s", time.UnixMilli(got).UTC(), time.UnixMilli(want).UTC())
	}
}

func TestCL2TestStartAbsentWithoutMarkers(t *testing.T) {
	dir := writeRun(t, map[string]string{
		"build-log.txt": "I0726 16:20:11.100000 1 clusterloader.go:230] no steps here\n",
	})
	if _, ok := cl2TestStart(dir, ms(t, "2026-07-26T16:15:00Z")); ok {
		t.Error("expected no step marker to be found")
	}
}

func TestCL2TestStartOnEmptyRunDir(t *testing.T) {
	if _, ok := cl2TestStart(t.TempDir(), ms(t, "2026-07-26T16:15:00Z")); ok {
		t.Error("expected no step marker in an empty run dir")
	}
}

// klog omits the year, so a run that straddles New Year must resolve against
// the neighbouring year rather than the hint's own.
func TestKlogTimeToMillisPicksNearestYear(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mm, dd, hms string
		hint, want  string
	}{
		{"same year", "07", "26", "16:23:51", "2026-07-26T16:15:00Z", "2026-07-26T16:23:51Z"},
		{"log in december, hint in january", "12", "31", "23:50:00", "2027-01-01T00:10:00Z", "2026-12-31T23:50:00Z"},
		{"log in january, hint in december", "01", "01", "00:10:00", "2026-12-31T23:50:00Z", "2027-01-01T00:10:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := klogTimeToMillis(tc.mm, tc.dd, tc.hms, ms(t, tc.hint))
			if !ok {
				t.Fatal("expected a resolved timestamp")
			}
			if want := ms(t, tc.want); got != want {
				t.Errorf("got %s, want %s", time.UnixMilli(got).UTC(), time.UnixMilli(want).UTC())
			}
		})
	}
}

// A leap day only parses against the leap year, so the nearest-year search must
// skip the candidates that fail to parse rather than give up on the line.
func TestKlogTimeToMillisLeapDay(t *testing.T) {
	got, ok := klogTimeToMillis("02", "29", "12:00:00", ms(t, "2028-03-01T00:00:00Z"))
	if !ok {
		t.Fatal("expected a resolved timestamp")
	}
	if want := ms(t, "2028-02-29T12:00:00Z"); got != want {
		t.Errorf("got %s, want %s", time.UnixMilli(got).UTC(), time.UnixMilli(want).UTC())
	}
}

// The anchor is the CL2 first step when the log is there, and falls back to the
// start of scraped data when it is not. Either way the offset must map the
// anchor exactly onto AnchorTime.
func TestRunAnchorPrefersCL2StepOverDataStart(t *testing.T) {
	dataStart := ms(t, "2026-07-26T16:15:00Z")

	withLog := writeRun(t, map[string]string{"build-log.txt": buildLog})
	got, src := runAnchor(withLog, dataStart)
	if want := ms(t, "2026-07-26T16:23:51Z"); got != want {
		t.Errorf("anchor: got %s, want %s", time.UnixMilli(got).UTC(), time.UnixMilli(want).UTC())
	}
	if src != "clusterloader2 first step" {
		t.Errorf("source: got %q", src)
	}

	got, src = runAnchor(t.TempDir(), dataStart)
	if got != dataStart {
		t.Errorf("fallback anchor: got %s, want %s", time.UnixMilli(got).UTC(), time.UnixMilli(dataStart).UTC())
	}
	if src != "start of scraped data" {
		t.Errorf("fallback source: got %q", src)
	}
}

// Samples scraped before the test starts must land before the origin, and the
// anchor itself must land exactly on it.
func TestTimebaseShiftPutsAnchorOnOrigin(t *testing.T) {
	tb := &Timebase{
		Enabled:  true,
		Known:    true,
		StartMS:  ms(t, "2026-07-26T16:15:00Z"),
		EndMS:    ms(t, "2026-07-26T17:44:36Z"),
		AnchorMS: ms(t, "2026-07-26T16:23:51Z"),
	}
	tb.OffsetMS = AnchorTimeMillis - tb.AnchorMS

	if got := tb.Shift(tb.AnchorMS); got != AnchorTimeMillis {
		t.Errorf("anchor shifted to %s, want %s",
			time.UnixMilli(got).UTC(), AnchorTime.Format(time.RFC3339))
	}
	if got := tb.NormalizedStart(); got >= AnchorTimeMillis {
		t.Errorf("pre-test data shifted to %s, want it before the origin", time.UnixMilli(got).UTC())
	}
	if got, want := tb.NormalizedEnd()-tb.NormalizedStart(), tb.EndMS-tb.StartMS; got != want {
		t.Errorf("duration changed under shift: got %d, want %d", got, want)
	}
}
