/*
Copyright 2018 The Kubernetes Authors.

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
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
	"k8s.io/kubernetes/test/e2e/perftype"
)

const (
	// S_TO_MS is a second to millisecond ratio.
	S_TO_MS = float64((time.Second) / time.Millisecond)
)

// BenchmarkResult is a dns benchmark results structure.
type BenchmarkResult struct {
	Code   int             `yaml:"code"`
	Data   BenchmarkData   `yaml:"data"`
	Params BenchmarkParams `yaml:"params"`
}

// BenchmarkData represents dns benchmark data.
type BenchmarkData struct {
	Latency50Percentile float64 `yaml:"latency_50_percentile"`
	Latency95Percentile float64 `yaml:"latency_95_percentile"`
	Latency99Percentile float64 `yaml:"latency_99_percentile"`
	AvgLatency          float64 `yaml:"avg_latency"`
	MaxLatency          float64 `yaml:"max_latency"`
	MinLatency          float64 `yaml:"min_latency"`
	Qps                 float64 `yaml:"qps"`
	QueriesCompleted    float64 `yaml:"queries_completed"`
	QueriesLost         float64 `yaml:"queries_lost"`
	QueriesSent         float64 `yaml:"queries_sent"`
}

// BenchmarkParams represents dns benchmark params.
type BenchmarkParams struct {
	RunLengthSeconds float64  `yaml:"run_length_seconds"`
	QueryFile        string   `yaml:"query_file"`
	KubednsCpu       *float64 `yaml:"kubedns_cpu"`
	DnsmasqCpu       *float64 `yaml:"dnsmasq_cpu"`
	DnsmasqCache     *float64 `yaml:"dnsmasq_cache"`
	MaxQps           *float64 `yaml:"max_qps"`
}

func main() {
	defer glog.Flush()
	err := run()
	if err != nil {
		panic(err)
	}
}

func run() error {
	var benchmarkDirPath, jsonDirPath, benchmarkName string
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flag.StringVar(&benchmarkDirPath, "benchmarkDirPath", ".", "benchmark results directory path")
	flag.StringVar(&jsonDirPath, "jsonDirPath", ".", "json results directory path")
	flag.StringVar(&benchmarkName, "benchmarkName", ".", "benchmark name")

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("flag parse failed: %v", err)
	}

	glog.Infof("benchmarkDirPath: %v\n", benchmarkDirPath)
	glog.Infof("jsonDirPath: %v\n", jsonDirPath)
	glog.Infof("benchmarkName: %v\n", benchmarkName)

	latency := perftype.PerfData{Version: "v1"}
	latencyPerc := perftype.PerfData{Version: "v1"}
	queries := perftype.PerfData{Version: "v1"}
	qps := perftype.PerfData{Version: "v1"}

	fileList, err := getFileList(benchmarkDirPath)
	if err != nil {
		return fmt.Errorf("listing files error: %v", err)
	}

	for _, file := range fileList {
		glog.Infof("processing %s\n", file)
		result, err := readBenchmarkResult(filepath.Join(benchmarkDirPath, file))
		if err != nil {
			return err
		}
		labels := createLabels(&result.Params)
		latency.DataItems = appendLatency(latency.DataItems, labels, result)
		latencyPerc.DataItems = appendLatencyPerc(latencyPerc.DataItems, labels, result)
		queries.DataItems = appendQueries(queries.DataItems, labels, result)
		qps.DataItems = appendQps(qps.DataItems, labels, result)
	}

	timeString := time.Now().Format(time.RFC3339)
	if err = saveMetric(&latency, filepath.Join(jsonDirPath, "Latency_"+benchmarkName+"_"+timeString+".json")); err != nil {
		return err
	}
	if err = saveMetric(&latencyPerc, filepath.Join(jsonDirPath, "LatencyPerc_"+benchmarkName+"_"+timeString+".json")); err != nil {
		return err
	}
	if err = saveMetric(&queries, filepath.Join(jsonDirPath, "Queries_"+benchmarkName+"_"+timeString+".json")); err != nil {
		return err
	}
	if err = saveMetric(&qps, filepath.Join(jsonDirPath, "Qps_"+benchmarkName+"_"+timeString+".json")); err != nil {
		return err
	}

	return nil
}

// getFileList returns a list of all files with extension .out.
func getFileList(dir string) ([]string, error) {
	var fileNames []string
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return fileNames, err
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".out" {
			fileNames = append(fileNames, file.Name())
		}
	}
	return fileNames, nil
}

func readBenchmarkResult(path string) (*BenchmarkResult, error) {
	var result BenchmarkResult
	bin, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading error: %v", err)
	}

	if err := yaml.Unmarshal(bin, &result); err != nil {
		return nil, fmt.Errorf("decoding failed: %v", err)
	}
	return &result, nil
}

func toString(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", *v)
}

func createLabels(params *BenchmarkParams) map[string]string {
	labels := make(map[string]string)
	labels["run_length_seconds"] = fmt.Sprintf("%v", params.RunLengthSeconds)
	labels["query_file"] = params.QueryFile
	labels["kubedns_cpu"] = toString(params.KubednsCpu)
	labels["dnsmasq_cpu"] = toString(params.DnsmasqCpu)
	labels["dnsmasq_cache"] = toString(params.DnsmasqCache)
	labels["max_qps"] = toString(params.MaxQps)
	return labels

}

func appendLatency(items []perftype.DataItem, labels map[string]string, result *BenchmarkResult) []perftype.DataItem {
	return append(items, perftype.DataItem{
		Unit:   "ms",
		Labels: labels,
		Data: map[string]float64{
			"max_latency": result.Data.MaxLatency * S_TO_MS,
			"avg_latency": result.Data.AvgLatency * S_TO_MS,
			"min_latency": result.Data.MinLatency * S_TO_MS,
		},
	})
}

func appendLatencyPerc(items []perftype.DataItem, labels map[string]string, result *BenchmarkResult) []perftype.DataItem {
	return append(items, perftype.DataItem{
		Unit:   "ms",
		Labels: labels,
		Data: map[string]float64{
			"perc50": result.Data.Latency50Percentile,
			"perc90": result.Data.Latency95Percentile,
			"perc99": result.Data.Latency99Percentile,
		},
	})
}

func appendQueries(items []perftype.DataItem, labels map[string]string, result *BenchmarkResult) []perftype.DataItem {
	return append(items, perftype.DataItem{
		Unit:   "",
		Labels: labels,
		Data: map[string]float64{
			"queries_completed": result.Data.QueriesCompleted,
			"queries_lost":      result.Data.QueriesLost,
			"queries_sent":      result.Data.QueriesSent,
		},
	})
}

func appendQps(items []perftype.DataItem, labels map[string]string, result *BenchmarkResult) []perftype.DataItem {
	return append(items, perftype.DataItem{
		Unit:   "1/s",
		Labels: labels,
		Data: map[string]float64{
			"qps": result.Data.Qps,
		},
	})
}

func saveMetric(metric *perftype.PerfData, path string) error {
	output := &bytes.Buffer{}
	if err := json.NewEncoder(output).Encode(metric); err != nil {
		return err
	}
	formatted := &bytes.Buffer{}
	if err := json.Indent(formatted, output.Bytes(), "", "  "); err != nil {
		return err
	}
	return ioutil.WriteFile(path, formatted.Bytes(), 0664)
}
