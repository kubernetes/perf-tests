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
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

var defaultLatencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}

// LogIngester parses kube-apiserver trace log files and ingests trace metrics into Thanos TSDB.
type LogIngester struct{}

// NewLogIngester creates a new instance of LogIngester.
func NewLogIngester() *LogIngester {
	return &LogIngester{}
}

// Ingest parses apiserver trace logs in runDir and generates OpenMetrics & Thanos TSDB block.
func (l *LogIngester) Ingest(ctx context.Context, runDir string, runName string, tsdbDir string, omDir string) error {
	logFiles, err := findAPIServerLogFiles(runDir)
	if err != nil || len(logFiles) == 0 {
		return fmt.Errorf("no kube-apiserver log files found in %s", runDir)
	}

	dateStr := autoDetectLogDate(logFiles)

	if err := os.MkdirAll(omDir, 0755); err != nil {
		return err
	}
	omFile := filepath.Join(omDir, fmt.Sprintf("openmetrics_log_traces_%s.txt", runName))

	fmt.Printf("Processing %d kube-apiserver log files for %s...\n", len(logFiles), runName)
	if err := processLogsAndWriteOpenMetrics(logFiles, omFile, dateStr); err != nil {
		return fmt.Errorf("processing logs and writing OpenMetrics: %w", err)
	}

	if err := createThanosTSDBBlockFromOpenMetrics(ctx, tsdbDir, runName, omFile); err != nil {
		return fmt.Errorf("creating Thanos TSDB block for logs: %w", err)
	}

	return nil
}

func findAPIServerLogFiles(runDir string) ([]string, error) {
	searchDir := filepath.Join(runDir, "artifacts")
	if _, err := os.Stat(searchDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("canonical artifacts directory does not exist: %s", searchDir)
	}

	var logFiles []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		filename := strings.ToLower(info.Name())
		if (strings.Contains(filename, "apiserver") || strings.Contains(filename, "kube-apiserver")) &&
			(strings.HasSuffix(filename, ".log") || strings.Contains(filename, ".log.")) {
			logFiles = append(logFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort log files in chronological order: .log.N -> .log.1 -> .log
	sort.Slice(logFiles, func(i, j int) bool {
		numI := extractLogNumber(logFiles[i])
		numJ := extractLogNumber(logFiles[j])
		if numI != numJ {
			return numI > numJ
		}
		return logFiles[i] < logFiles[j]
	})

	return logFiles, nil
}

func extractLogNumber(path string) int {
	ext := filepath.Ext(path)
	ext = strings.TrimPrefix(ext, ".")
	if num, err := strconv.Atoi(ext); err == nil {
		return num
	}
	return 0
}

func autoDetectLogDate(files []string) string {
	dateRe := regexp.MustCompile(`[IWEF](\d{4})\s+(\d{2}:\d{2}:\d{2})`)
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			matches := dateRe.FindStringSubmatch(scanner.Text())
			if len(matches) == 3 {
				mmdd := matches[1]
				year := time.Now().UTC().Format("2006")
				f.Close()
				return fmt.Sprintf("%s-%s-%s", year, mmdd[:2], mmdd[2:])
			}
		}
		f.Close()
	}
	return time.Now().UTC().Format("2006-01-02")
}

type traceRecord struct {
	timestamp string
	name      string
	group     string
	resource  string
	totalMS   int64
	steps     map[string]int64
}

func processLogsAndWriteOpenMetrics(logFiles []string, outputFile string, dateStr string) error {
	var globalTraces []traceRecord
	minTimestamp := "23:59:59"
	maxTimestamp := "00:00:00"

	timestampSecData := make(map[int64]map[string]int64)

	traceStartRe := regexp.MustCompile(`[IWEF]\d{4}\s+(\d{2}:\d{2}:\d{2}\.\d+).*?Trace\[(\d+)\]:\s*\\?"?([^"\\\s]+)`)
	traceEndRe := regexp.MustCompile(`Trace\[(\d+)\]:\s*\[([0-9\.]+[a-zA-Zµ]+)\]\s*\[([0-9\.]+[a-zA-Zµ]+)\]\s*END`)
	traceStepRe := regexp.MustCompile(`Trace\[(\d+)\]:\s*(?:\[|---\s*)\\?"?([^\("\\]+?)\\?"?\s+([0-9\.]+[a-zA-Zµ]+)`)
	logLineRe := regexp.MustCompile(`^[IWEF](\d{4})\s+(\d{2}:\d{2}:\d{2})`)

	activeTraces := make(map[string]*traceRecord)

	for _, file := range logFiles {
		f, err := os.Open(file)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 1024*1024)
		scanner.Buffer(buf, 10*1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			lineLen := int64(len(line))

			matches := logLineRe.FindStringSubmatch(line)
			if len(matches) == 3 {
				ts := matches[2]
				if ts < minTimestamp {
					minTimestamp = ts
				}
				if ts > maxTimestamp {
					maxTimestamp = ts
				}

				epoch := convertToEpoch(dateStr, ts)
				if _, ok := timestampSecData[epoch]; !ok {
					timestampSecData[epoch] = make(map[string]int64)
				}

				levelChar := line[0:1]
				switch levelChar {
				case "I":
					timestampSecData[epoch]["info_lines"]++
					timestampSecData[epoch]["info_bytes"] += lineLen
				case "W":
					timestampSecData[epoch]["warn_lines"]++
					timestampSecData[epoch]["warn_bytes"] += lineLen
				case "E":
					timestampSecData[epoch]["error_lines"]++
					timestampSecData[epoch]["error_bytes"] += lineLen
				case "F":
					timestampSecData[epoch]["fatal_lines"]++
					timestampSecData[epoch]["fatal_bytes"] += lineLen
				default:
					timestampSecData[epoch]["other_lines"]++
					timestampSecData[epoch]["other_bytes"] += lineLen
				}
			}

			if endMatch := traceEndRe.FindStringSubmatch(line); len(endMatch) == 4 {
				id := endMatch[1]
				durStr := endMatch[3]
				if tr, ok := activeTraces[id]; ok {
					tr.totalMS = parseDurationMS(durStr)
					globalTraces = append(globalTraces, *tr)
					delete(activeTraces, id)
				}
			} else if startMatch := traceStartRe.FindStringSubmatch(line); len(startMatch) == 4 {
				ts := startMatch[1]
				if idx := strings.Index(ts, "."); idx != -1 {
					ts = ts[:idx]
				}
				id := startMatch[2]
				rawName := strings.Trim(startMatch[3], `"`)

				name, group, resource := parseTraceLabels(rawName, line)
				tr, ok := activeTraces[id]
				if !ok {
					tr = &traceRecord{
						steps: make(map[string]int64),
					}
					activeTraces[id] = tr
				}
				tr.timestamp = ts
				tr.name = name
				tr.group = group
				tr.resource = resource
			} else if stepMatch := traceStepRe.FindStringSubmatch(line); len(stepMatch) == 4 {
				id := stepMatch[1]
				stepName := strings.TrimSpace(strings.Trim(stepMatch[2], `"`))
				durStr := stepMatch[3]
				tr, ok := activeTraces[id]
				if !ok {
					tr = &traceRecord{
						steps: make(map[string]int64),
					}
					activeTraces[id] = tr
				}
				tr.steps[stepName] = parseDurationMS(durStr)
			}
		}
		f.Close()
	}

	fmt.Printf("Parsed %d completed log traces from %d log files.\n", len(globalTraces), len(logFiles))
	return writeOpenMetricsFile(outputFile, dateStr, minTimestamp, maxTimestamp, timestampSecData, globalTraces)
}

func parseTraceLabels(rawName string, line string) (name, group, resource string) {
	name = rawName
	group = ""
	resource = ""

	resourceRe := regexp.MustCompile(`resource:([a-zA-Z0-9_\-\.\/]+)`)
	if matches := resourceRe.FindStringSubmatch(line); len(matches) == 2 {
		resource = matches[1]
	}

	groupRe := regexp.MustCompile(`api-group:([a-zA-Z0-9_\-\.]+)`)
	if matches := groupRe.FindStringSubmatch(line); len(matches) == 2 {
		group = matches[1]
	}

	return name, group, resource
}

func parseDurationMS(durStr string) int64 {
	if strings.HasSuffix(durStr, "µs") || strings.HasSuffix(durStr, "us") {
		clean := strings.TrimSuffix(strings.TrimSuffix(durStr, "µs"), "us")
		val, _ := strconv.ParseFloat(clean, 64)
		return int64(val / 1000.0)
	} else if strings.HasSuffix(durStr, "ns") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(durStr, "ns"), 64)
		return int64(val / 1000000.0)
	} else if strings.HasSuffix(durStr, "ms") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(durStr, "ms"), 64)
		return int64(val)
	} else if strings.HasSuffix(durStr, "s") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(durStr, "s"), 64)
		return int64(val * 1000.0)
	}
	return 0
}

func writeOpenMetricsFile(outputFile string, dateStr string, minTS string, maxTS string, timestampSecData map[int64]map[string]int64, globalTraces []traceRecord) error {
	var lines []string

	lines = append(lines, "# HELP apiserver_log_volume_lines_total Total log lines by severity")
	lines = append(lines, "# TYPE apiserver_log_volume_lines_total counter")
	lines = append(lines, "# HELP apiserver_log_volume_bytes_total Total log volume in bytes by severity")
	lines = append(lines, "# TYPE apiserver_log_volume_bytes_total counter")
	lines = append(lines, "# HELP apiserver_log_traces_duration_seconds APIServer trace duration seconds histogram")
	lines = append(lines, "# TYPE apiserver_log_traces_duration_seconds histogram")

	timelineSecs := getTimelineSeconds(minTS, maxTS)
	var cumInfoLines, cumInfoBytes int64
	var cumWarnLines, cumWarnBytes int64
	var cumErrorLines, cumErrorBytes int64
	var cumFatalLines, cumFatalBytes int64
	var cumOtherLines, cumOtherBytes int64

	for _, tsStr := range timelineSecs {
		epoch := convertToEpoch(dateStr, tsStr)
		if m, ok := timestampSecData[epoch]; ok {
			cumInfoLines += m["info_lines"]
			cumInfoBytes += m["info_bytes"]
			cumWarnLines += m["warn_lines"]
			cumWarnBytes += m["warn_bytes"]
			cumErrorLines += m["error_lines"]
			cumErrorBytes += m["error_bytes"]
			cumFatalLines += m["fatal_lines"]
			cumFatalBytes += m["fatal_bytes"]
			cumOtherLines += m["other_lines"]
			cumOtherBytes += m["other_bytes"]
		}
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_lines_total{level="info"} %d %d`, cumInfoLines, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_bytes_total{level="info"} %d %d`, cumInfoBytes, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_lines_total{level="warn"} %d %d`, cumWarnLines, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_bytes_total{level="warn"} %d %d`, cumWarnBytes, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_lines_total{level="error"} %d %d`, cumErrorLines, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_bytes_total{level="error"} %d %d`, cumErrorBytes, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_lines_total{level="fatal"} %d %d`, cumFatalLines, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_bytes_total{level="fatal"} %d %d`, cumFatalBytes, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_lines_total{level="other"} %d %d`, cumOtherLines, epoch))
		lines = append(lines, fmt.Sprintf(`apiserver_log_volume_bytes_total{level="other"} %d %d`, cumOtherBytes, epoch))
	}

	type seriesKey struct {
		name     string
		group    string
		resource string
		step     string
	}

	seriesEpochDurations := make(map[seriesKey]map[int64][]float64)

	for _, tr := range globalTraces {
		epoch := convertToEpoch(dateStr, tr.timestamp)
		kBase := seriesKey{name: tr.name, group: tr.group, resource: tr.resource, step: ""}
		if _, ok := seriesEpochDurations[kBase]; !ok {
			seriesEpochDurations[kBase] = make(map[int64][]float64)
		}
		seriesEpochDurations[kBase][epoch] = append(seriesEpochDurations[kBase][epoch], float64(tr.totalMS)/1000.0)

		for stepName, stepMS := range tr.steps {
			kStep := seriesKey{name: tr.name, group: tr.group, resource: tr.resource, step: stepName}
			if _, ok := seriesEpochDurations[kStep]; !ok {
				seriesEpochDurations[kStep] = make(map[int64][]float64)
			}
			seriesEpochDurations[kStep][epoch] = append(seriesEpochDurations[kStep][epoch], float64(stepMS)/1000.0)
		}
	}

	var seriesKeys []seriesKey
	for sk := range seriesEpochDurations {
		seriesKeys = append(seriesKeys, sk)
	}

	sort.Slice(seriesKeys, func(i, j int) bool {
		if seriesKeys[i].name != seriesKeys[j].name {
			return seriesKeys[i].name < seriesKeys[j].name
		}
		if seriesKeys[i].group != seriesKeys[j].group {
			return seriesKeys[i].group < seriesKeys[j].group
		}
		if seriesKeys[i].resource != seriesKeys[j].resource {
			return seriesKeys[i].resource < seriesKeys[j].resource
		}
		return seriesKeys[i].step < seriesKeys[j].step
	})

	type traceHistState struct {
		bucketCounts map[float64]int64
		infCount     int64
		sum          float64
		count        int64
	}

	for _, sk := range seriesKeys {
		epochMap := seriesEpochDurations[sk]
		var epochs []int64
		for ep := range epochMap {
			epochs = append(epochs, ep)
		}
		sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })

		st := &traceHistState{bucketCounts: make(map[float64]int64)}

		for _, ep := range epochs {
			durations := epochMap[ep]
			for _, d := range durations {
				st.sum += d
				st.count++
				st.infCount++
				for _, b := range defaultLatencyBuckets {
					if d <= b {
						st.bucketCounts[b]++
					}
				}
			}

			for _, b := range defaultLatencyBuckets {
				lines = append(lines, fmt.Sprintf(`apiserver_log_traces_duration_seconds_bucket{name="%s",group="%s",resource="%s",step="%s",le="%s"} %d %d`,
					sk.name, sk.group, sk.resource, sk.step, formatBucketLe(b), st.bucketCounts[b], ep))
			}
			lines = append(lines, fmt.Sprintf(`apiserver_log_traces_duration_seconds_bucket{name="%s",group="%s",resource="%s",step="%s",le="+Inf"} %d %d`,
				sk.name, sk.group, sk.resource, sk.step, st.infCount, ep))
			lines = append(lines, fmt.Sprintf(`apiserver_log_traces_duration_seconds_sum{name="%s",group="%s",resource="%s",step="%s"} %.3f %d`,
				sk.name, sk.group, sk.resource, sk.step, st.sum, ep))
			lines = append(lines, fmt.Sprintf(`apiserver_log_traces_duration_seconds_count{name="%s",group="%s",resource="%s",step="%s"} %d %d`,
				sk.name, sk.group, sk.resource, sk.step, st.count, ep))
		}
	}

	lines = append(lines, "# EOF\n")

	content := strings.Join(lines, "\n")
	return os.WriteFile(outputFile, []byte(content), 0644)
}

func getTimelineSeconds(minTS, maxTS string) []string {
	partsMin := strings.Split(minTS, ":")
	partsMax := strings.Split(maxTS, ":")
	if len(partsMin) != 3 || len(partsMax) != 3 {
		return nil
	}

	hMin, _ := strconv.Atoi(partsMin[0])
	mMin, _ := strconv.Atoi(partsMin[1])
	sMin, _ := strconv.Atoi(partsMin[2])

	hMax, _ := strconv.Atoi(partsMax[0])
	mMax, _ := strconv.Atoi(partsMax[1])
	sMax, _ := strconv.Atoi(partsMax[2])

	var timeline []string
	curH, curM, curS := hMin, mMin, sMin

	for (curH < hMax) || (curH == hMax && curM < mMax) || (curH == hMax && curM == mMax && curS <= sMax) {
		timeline = append(timeline, fmt.Sprintf("%02d:%02d:%02d", curH, curM, curS))
		curS += 15
		if curS >= 60 {
			curS = 0
			curM++
			if curM >= 60 {
				curM = 0
				curH++
			}
		}
	}
	return timeline
}

func convertToEpoch(dateStr, tsStr string) int64 {
	t, err := time.Parse("2006-01-02 15:04:05", dateStr+" "+tsStr)
	if err != nil {
		return time.Now().UTC().Unix()
	}
	return t.Unix()
}

func createThanosTSDBBlockFromOpenMetrics(ctx context.Context, tsdbDir string, runName string, openmetricsFilePath string) error {
	file, err := os.Open(openmetricsFilePath)
	if err != nil {
		return fmt.Errorf("opening openmetrics file %s: %w", openmetricsFilePath, err)
	}
	defer file.Close()

	tsdbDirAbs, err := filepath.Abs(tsdbDir)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bw, err := tsdb.NewBlockWriter(logger, tsdbDirAbs, 2*60*60*1000)
	if err != nil {
		return fmt.Errorf("creating TSDB BlockWriter: %w", err)
	}
	defer bw.Close()

	app := bw.Appender(ctx)

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)

	samplesCount := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, lblsMap, valFloat, tsMS, err := parseOpenMetricsSampleLine(line, runName)
		if err != nil {
			continue
		}
		lblsMap["__name__"] = name

		if _, err := app.Append(0, labels.FromMap(lblsMap), tsMS, valFloat); err != nil {
			return fmt.Errorf("appending sample %s to TSDB: %w", name, err)
		}
		samplesCount++
	}

	if samplesCount == 0 {
		return fmt.Errorf("no valid samples parsed from %s", openmetricsFilePath)
	}

	if err := app.Commit(); err != nil {
		return fmt.Errorf("committing TSDB appender: %w", err)
	}

	blockULID, err := bw.Flush(ctx)
	if err != nil {
		return fmt.Errorf("writing TSDB block: %w", err)
	}

	blockDir := filepath.Join(tsdbDirAbs, blockULID.String())
	thanosMeta := metadata.Thanos{
		Labels: map[string]string{
			"run": runName,
		},
		Source: metadata.SourceType("perflens-log-ingest"),
	}

	nopLog := nopLogger{}
	if _, err := metadata.InjectThanos(nopLog, blockDir, thanosMeta, nil); err != nil {
		return fmt.Errorf("injecting Thanos metadata into block %s: %w", blockULID.String(), err)
	}

	fmt.Printf("Native Thanos TSDB block for log traces created successfully: ULID %s in %s (%d samples)\n", blockULID.String(), tsdbDirAbs, samplesCount)
	return nil
}

func parseOpenMetricsSampleLine(line string, runName string) (string, map[string]string, float64, int64, error) {
	lastBrace := strings.LastIndex(line, "}")
	var metricPart, restStr string
	if lastBrace != -1 {
		metricPart = line[:lastBrace+1]
		restStr = strings.TrimSpace(line[lastBrace+1:])
	} else {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", nil, 0, 0, fmt.Errorf("invalid line")
		}
		metricPart = fields[0]
		restStr = strings.Join(fields[1:], " ")
	}

	fields := strings.Fields(restStr)
	if len(fields) < 1 {
		return "", nil, 0, 0, fmt.Errorf("missing value")
	}
	valFloat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", nil, 0, 0, err
	}

	tsMS := time.Now().UTC().UnixMilli()
	if len(fields) >= 2 {
		if tsSec, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			tsMS = tsSec * 1000
		}
	}

	lblsMap := map[string]string{"run": runName}
	name := metricPart

	if idx := strings.Index(metricPart, "{"); idx != -1 {
		name = metricPart[:idx]
		labelsStr := metricPart[idx+1 : len(metricPart)-1]

		kvRe := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*"([^"]*)"`)
		matches := kvRe.FindAllStringSubmatch(labelsStr, -1)
		for _, m := range matches {
			if len(m) == 3 {
				lblsMap[m[1]] = m[2]
			}
		}
	}

	return name, lblsMap, valFloat, tsMS, nil
}

func formatBucketLe(b float64) string {
	if b == float64(int64(b)) {
		return fmt.Sprintf("%.1f", b)
	}
	return strconv.FormatFloat(b, 'f', -1, 64)
}
