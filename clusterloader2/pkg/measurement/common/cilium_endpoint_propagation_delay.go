/*
Copyright 2022 The Kubernetes Authors.

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
	"math"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	cepPropagationDelayMeasurementName = "CiliumEndpointPropagationDelay"
	// The metric definition and bucket sizes for cilium_endpoint_propagation_delay_seconds:
	// https://github.com/cilium/cilium/blob/v1.11/pkg/metrics/metrics.go#L1263
	cepPropagationDelayQuery = `sum(cilium_endpoint_propagation_delay_seconds_bucket) by (le)`
	queryInterval            = 10 * time.Minute

	// bucketAllEntries is the default Prometheus bucket that
	// includes all entries for the histogram snapshot.
	bucketAllEntries = "+Inf"
	// defaultBucketSLO and defaultPercentileSLO are used together to
	// determine if the test should pass, when not specified otherwise
	// in the CL2 parameters. The test should pass only if the size of
	// the defaultBucketSLO is at least within the defined percentile
	// in comparison to the size of the bucketAllEntries.
	defaultBucketSLO             = 600
	defaultPercentileSLO float64 = 95
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&cepPropagationDelayGatherer{})
	}
	if err := measurement.Register(cepPropagationDelayMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", cepPropagationDelayMeasurementName, err)
	}
}

type cepPropagationDelayGatherer struct{}

// cepPropagationDelayMetricMap contains timestamps in the outer map,
// and buckets and their sizes in the inner map.
type cepPropagationDelayMetricMap map[string]map[string]int

func (c *cepPropagationDelayGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	cepPropagationDelay, err := c.gatherCepPropagationDelay(executor, startTime, endTime)
	if err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(cepPropagationDelay)
	if err != nil {
		return nil, err
	}

	summaries := []measurement.Summary{measurement.CreateSummary(cepPropagationDelayMeasurementName, "json", content)}
	return summaries, validateCEPPropagationDelay(cepPropagationDelay, config)
}

func (c *cepPropagationDelayGatherer) gatherCepPropagationDelay(executor QueryExecutor, startTime, endTime time.Time) (cepPropagationDelayMetricMap, error) {
	// Query the data between start and end time on fixed intervals
	// to get accurate data from multiple snapshots.
	var samples []*model.Sample
	queryTime := startTime.Add(queryInterval)
	for queryTime.Before(endTime) {
		newSamples, err := executor.Query(cepPropagationDelayQuery, queryTime)
		if err == nil {
			samples = append(samples, newSamples...)
		} else {
			klog.V(2).Infof("Got error querying Prometheus: %v", err)
		}
		queryTime = queryTime.Add(queryInterval)
	}

	extractSampleData := func(sample *model.Sample) (string, string, int) {
		return sample.Timestamp.String(), string(sample.Metric["le"]), int(math.Round(float64(sample.Value)))
	}

	result := make(cepPropagationDelayMetricMap)
	for _, sample := range samples {
		timestamp, bucket, value := extractSampleData(sample)
		if _, ok := result[timestamp]; !ok {
			result[timestamp] = make(map[string]int)
		}
		result[timestamp][bucket] = value
	}
	return result, nil
}

func validateCEPPropagationDelay(result cepPropagationDelayMetricMap, config *measurement.Config) error {
	bucketNumSLO, err := util.GetFloat64OrDefault(config.Params, "bucketSLO", defaultBucketSLO)
	if err != nil || bucketNumSLO == 0 {
		klog.V(2).Infof("Using defaultBucketSLO: %d, because bucketSLO param is invalid: %v", int(math.Floor(defaultBucketSLO)), err)
		bucketNumSLO = defaultBucketSLO
	}
	bucketSLO := strconv.FormatFloat(bucketNumSLO, 'g', -1, 64)

	percentileSLO, err := util.GetFloat64OrDefault(config.Params, "percentileSLO", defaultPercentileSLO)
	if err != nil || percentileSLO == 0 {
		klog.V(2).Infof("Using defaultPercentileSLO: %f, because percentileSLO param is invalid: %v", percentileSLO, err)
		percentileSLO = defaultPercentileSLO
	}

	for timestamp, buckets := range result {
		totalEvents := buckets[bucketAllEntries]
		if totalEvents == 0 {
			continue
		}

		acceptedDelayEvents := buckets[bucketSLO]
		perc := (float64(acceptedDelayEvents) / float64(totalEvents)) * 100
		if perc < percentileSLO {
			return errors.NewMetricViolationError(
				"Cilium endpoint propagation delay",
				fmt.Sprintf("%s: updates for %ss delay is within %d%%, expected %d%%, buckets:\n%v",
					timestamp,
					bucketSLO,
					int(math.Floor(perc)),
					int(math.Floor(percentileSLO)),
					buckets,
				),
			)
		}
	}
	return nil
}

func (c *cepPropagationDelayGatherer) Configure(config *measurement.Config) error {
	return nil
}

func (c *cepPropagationDelayGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}

func (*cepPropagationDelayGatherer) String() string {
	return cepPropagationDelayMeasurementName
}
