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

package common

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	schedulingThroughputPrometheusMeasurementName = "SchedulingThroughputPrometheus"

	maxSchedulingThroughputQuery = `max_over_time(sum(irate(apiserver_request_total{verb="POST", resource="pods", subresource="binding",code="201"}[1m]))[%v:5s])`
)

func init() {
	create := func() measurement.Measurement { return CreatePrometheusMeasurement(&schedulingThroughputGatherer{}) }
	if err := measurement.Register(schedulingThroughputPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", schedulingThroughputMeasurementName, err)
	}
}

type schedulingThroughputGatherer struct{}

func (a *schedulingThroughputGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	throughputSummary, err := a.getThroughputSummary(executor, startTime, endTime, config)
	if err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(throughputSummary)
	if err != nil {
		return nil, err
	}

	threshold, err := util.GetFloat64OrDefault(config.Params, "threshold", 0)
	if threshold > 0 && throughputSummary.Max < threshold {
		err = errors.NewMetricViolationError(
			"scheduler throughput_prometheus",
			fmt.Sprintf("actual throughput %f lower than threshold %f", throughputSummary.Max, threshold))
	}

	summaries := []measurement.Summary{
		measurement.CreateSummary(a.String(), "json", content),
	}

	return summaries, err
}

func (a *schedulingThroughputGatherer) getThroughputSummary(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) (*schedulingThroughputPrometheus, error) {
	measurementDuration := endTime.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)
	query := fmt.Sprintf(maxSchedulingThroughputQuery, promDuration)

	samples, err := executor.Query(query, endTime)
	if err != nil {
		return nil, err
	}
	if len(samples) != 1 {
		return nil, fmt.Errorf("got unexpected number of samples: %d", len(samples))
	}

	maxSchedulingThroughput := samples[0].Value
	throughputSummary := &schedulingThroughputPrometheus{
		Max: float64(maxSchedulingThroughput),
	}

	return throughputSummary, nil
}

func (a *schedulingThroughputGatherer) String() string {
	return schedulingThroughputPrometheusMeasurementName
}

func (a *schedulingThroughputGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (a *schedulingThroughputGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

type schedulingThroughputPrometheus struct {
	Max float64 `json:"max"`
}
