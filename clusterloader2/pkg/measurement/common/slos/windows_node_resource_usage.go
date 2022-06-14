/*
Copyright 2020 The Kubernetes Authors.

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

package slos

import (
	"math"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	windowsResourceUsagePrometheusMeasurementName = "WindowsResourceUsagePrometheus"
	// get top 10 non-system processes with highest cpu usage within 1min query window size
	cpuUsageQueryTop10 = `topk(10, sum by (process) (irate(windows_process_cpu_time_total{process!~"Idle|Total|System"}[5m]) / on(job) group_left windows_cs_logical_processors) * 100)`
	// cpu usage metrics file name prefix
	cpuUsageMetricsName = "WindowsCPUUsagePrometheus"
	// get top 10 non-system processes with highest memory usage
	memoryUsageQueryTop10 = `topk(10, sum(windows_process_working_set_bytes{process!~"Idle|Total|System"}) by (process))`
	// memory usage metrics file name prefix
	memoryUsageMetricsName = "WindowsMemoryUsagePrometheus"
	// get the total disk size by volume
	nodeStorageUsageQuery = `sum(windows_logical_disk_size_bytes) by (volume)`
	// node storage usage file name prefix
	nodeStorageUsageMetricsName = "WindowsNodeStorage"
	// get the total open file descriptors
	openFilesQuery = `sum(process_open_fds{service="windows-exporter"})`
	// open files metrics file name prefix
	openFilesMetricsName = "WindowsOpenFiles"
	// bytes issued to I/O operations
	processQueryTop10 = `topk(10, sum by (process) (windows_process_io_bytes_total{process!~"Idle|Total|System"}))`
	// process metrics file name prefix
	processMetricsName = "WindowsProcesses"
	// number of Windows containers
	containerQueryTop10 = `topk(10, sum by (instance) (windows_container_count))`
	// container metrics file name prefix
	containerMetricsName = "WindowsContainers"
	// total bytes received and transmitted by interface
	networkQueryTop10 = `topk(10, sum by (nic) (windows_net_bytes_total))`
	// network metrics file name prefix
	networkMetricsName                        = "WindowsNetwork"
	currentWindowsResourceUsageMetricsVersion = "v1"
)

type convertFunc func([]*model.Sample) *measurementutil.PerfData
type windowsResourceUsageGatherer struct{}

func (w *windowsResourceUsageGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (w *windowsResourceUsageGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (w *windowsResourceUsageGatherer) String() string {
	return windowsResourceUsagePrometheusMeasurementName
}

func init() {
	create := func() measurement.Measurement {
		return common.CreatePrometheusMeasurement(&windowsResourceUsageGatherer{})
	}
	if err := measurement.Register(windowsResourceUsagePrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", windowsResourceUsagePrometheusMeasurementName, err)
	}
}

func convertToCPUPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"CPU_Usage": math.Round(float64(sample.Value)*100) / 100,
			},
			Unit: "%",
			Labels: map[string]string{
				"Process": string(sample.Metric["process"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToMemoryPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Memory_Usage": math.Round(float64(sample.Value)*100/(1024*1024)) / 100,
			},
			Unit: "MB",
			Labels: map[string]string{
				"Process": string(sample.Metric["process"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToStoragePerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Storage_Used": math.Round(float64(sample.Value)*100/(1024*1024*1024)) / 100,
			},
			Unit: "GB",
			Labels: map[string]string{
				"Volume": string(sample.Metric["volume"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToOpenFilesPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Open File Handles": math.Round(float64(sample.Value)),
			},
			Unit: "Handles",
			Labels: map[string]string{
				"Job": string(sample.Metric["job"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToProcessPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Process Handles": math.Round(float64(sample.Value)*100) / 100,
			},
			Unit: "Handles",
			Labels: map[string]string{
				"Process": string(sample.Metric["process"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToNetworkPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Network_Bytes_Total": math.Round(float64(sample.Value)*100/(1024*1024)) / 100,
			},
			Unit: "MB",
			Labels: map[string]string{
				"NIC": string(sample.Metric["nic"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}

func convertToContainerPerfData(samples []*model.Sample) *measurementutil.PerfData {
	perfData := &measurementutil.PerfData{Version: currentWindowsResourceUsageMetricsVersion}
	for _, sample := range samples {
		item := measurementutil.DataItem{
			Data: map[string]float64{
				"Windows Container Count": math.Round(float64(sample.Value)*100/(1024*1024)) / 100,
			},
			Unit: "Containers",
			Labels: map[string]string{
				"Instance": string(sample.Metric["instance"]),
			},
		}
		perfData.DataItems = append(perfData.DataItems, item)
	}
	return perfData
}
func getSummary(query string, converter convertFunc, metricsName string, measurementTime time.Time, executor common.QueryExecutor, config *measurement.Config) (measurement.Summary, error) {
	samples, err := executor.Query(query, measurementTime)
	if err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(converter(samples))
	if err != nil {
		return nil, err
	}
	summaryName, err := util.GetStringOrDefault(config.Params, "summaryName", metricsName)
	if err != nil {
		return nil, err
	}
	return measurement.CreateSummary(summaryName, "json", content), nil
}

// Gather gathers the metrics and convert to json summary
func (w *windowsResourceUsageGatherer) Gather(executor common.QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	cpuSummary, err := getSummary(cpuUsageQueryTop10, convertToCPUPerfData, cpuUsageMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	memorySummary, err := getSummary(memoryUsageQueryTop10, convertToMemoryPerfData, memoryUsageMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	nodeStorageSummary, err := getSummary(nodeStorageUsageQuery, convertToStoragePerfData, nodeStorageUsageMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	openFilesSummary, err := getSummary(openFilesQuery, convertToOpenFilesPerfData, openFilesMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	processSummary, err := getSummary(processQueryTop10, convertToProcessPerfData, processMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	networkSummary, err := getSummary(networkQueryTop10, convertToNetworkPerfData, networkMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	containerSummary, err := getSummary(containerQueryTop10, convertToContainerPerfData, containerMetricsName, endTime, executor, config)
	if err != nil {
		return nil, err
	}
	return []measurement.Summary{cpuSummary, memorySummary, nodeStorageSummary, openFilesSummary, processSummary, networkSummary, containerSummary}, nil
}
