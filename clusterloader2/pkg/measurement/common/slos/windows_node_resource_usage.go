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
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	windowsResourceUsagePrometheusMeasurementName = "WindowsResourceUsagePrometheus"
	// get top 10 non-system processes with highest cpu usage within 1min query window size
	cpuUsageQueryTop10                        = `topk(10, sum by (process) (irate(wmi_process_cpu_time_total{process!~"Idle|Total|System"}[5m]) / on(job) group_left wmi_cs_logical_processors) * 100)`
	currentWindowsResourceUsageMetricsVersion = "v1"
)

type windowsResourceUsageGatherer struct{}

func (w *windowsResourceUsageGatherer) IsEnabled(config *measurement.MeasurementConfig) bool {
	return true
}

func (w *windowsResourceUsageGatherer) String() string {
	return windowsResourceUsagePrometheusMeasurementName
}

func init() {
	create := func() measurement.Measurement { return createPrometheusMeasurement(&windowsResourceUsageGatherer{}) }
	if err := measurement.Register(windowsResourceUsagePrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", windowsResourceUsagePrometheusMeasurementName, err)
	}
}

func convertToPerfData(samples []*model.Sample) *measurementutil.PerfData {
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

// Gather gathers the metrics and convert to json summary
func (w *windowsResourceUsageGatherer) Gather(executor QueryExecutor, startTime time.Time, config *measurement.MeasurementConfig) (measurement.Summary, error) {
	samples, err := executor.Query(cpuUsageQueryTop10, time.Now())
	if err != nil {
		return nil, err
	}
	content, err := util.PrettyPrintJSON(convertToPerfData(samples))
	if err != nil {
		return nil, err
	}
	summaryName, err := util.GetStringOrDefault(config.Params, "summaryName", windowsResourceUsagePrometheusMeasurementName)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(summaryName, "json", content)
	return summary, nil
}
