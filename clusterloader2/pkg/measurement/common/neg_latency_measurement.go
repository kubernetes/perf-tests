/*
Copyright 2024 The Kubernetes Authors.

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
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	negLatencyMeasurementName   = "NegLatency"
	negInitLatencyQuery         = `sum(sum_over_time(neg_controller_neg_initialization_duration_seconds_bucket[%v])) by (le)`
	negOpLatencyQuery           = `sum(sum_over_time(neg_controller_neg_operation_duration_seconds_bucket{operation="%v"}[%v])) by (le)`
	negInitializationMetricName = "neg_initialization"
	negAttachOpMetricName       = "neg_attach_endpoint"
	negDetachOpMetricName       = "neg_detach_endpoint"
	negAttachOpKey              = "Attach"
	negDetachOpKey              = "Detach"
	negInfBucketSLO             = "+Inf"

	negQueryInterval = 10 * time.Minute

	defaultNegInitBucketSLO       float64 = 16
	defaultNegInitPercentileSLO   float64 = 95
	defaultNegAttachBucketSLO     float64 = 64
	defaultNegAttachPercentileSLO float64 = 95
	defaultNegDetachBucketSLO     float64 = 32
	defaultNegDetachPercentileSLO float64 = 95
)

func init() {
	create := func() measurement.Measurement {
		return CreatePrometheusMeasurement(&negLatencyGatherer{})
	}
	if err := measurement.Register(negLatencyMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", negLatencyMeasurementName, err)
	}
}

type negLatencyGatherer struct{}

type negLatencyMetricMap map[string]map[string]int

func (g *negLatencyGatherer) Gather(executor QueryExecutor, startTime, endTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	negMetrics := g.gatherMetrics(executor, startTime, endTime)

	content, err := util.PrettyPrintJSON(negMetrics)
	if err != nil {
		return nil, err
	}

	summaries := []measurement.Summary{measurement.CreateSummary(negLatencyMeasurementName, "json", content)}
	return summaries, validateNegSLOs(negMetrics, config)
}

func validateNegSLOs(result negLatencyMetricMap, config *measurement.Config) error {
	if err := validateNegSLO(result, negInitializationMetricName, "negInitBucketSLO", "negInitPercentileSLO", defaultNegInitBucketSLO, defaultNegInitPercentileSLO, config); err != nil {
		return err
	}
	if err := validateNegSLO(result, negAttachOpMetricName, "negAttachBucketSLO", "negAttachPercentileSLO", defaultNegAttachBucketSLO, defaultNegAttachPercentileSLO, config); err != nil {
		return err
	}
	if err := validateNegSLO(result, negDetachOpMetricName, "negDetachBucketSLO", "negDetachPercentileSLO", defaultNegDetachBucketSLO, defaultNegDetachPercentileSLO, config); err != nil {
		return err
	}
	return nil
}

func validateNegSLO(result negLatencyMetricMap, metricName, bucketName, percentileName string, defaultBucketSLO, defaultPercentileSLO float64, config *measurement.Config) error {
	bucketNumSLO, err := util.GetFloat64OrDefault(config.Params, bucketName, defaultBucketSLO)
	if err != nil || bucketNumSLO == 0 {
		klog.V(2).Infof("Using default value for %s: %f, because %s param is invalid: %v", bucketName, defaultBucketSLO, bucketName, err)
		bucketNumSLO = defaultBucketSLO
	}
	bucketSLO := strconv.FormatFloat(bucketNumSLO, 'g', -1, 64)

	percentileSLO, err := util.GetFloat64OrDefault(config.Params, percentileName, defaultPercentileSLO)
	if err != nil || percentileSLO == 0 {
		klog.V(2).Infof("Using default value for %s: %f, because %s param is invalid: %v", percentileName, defaultPercentileSLO, percentileName, err)
		percentileSLO = defaultPercentileSLO
	}

	if buckets, ok := result[metricName]; ok {
		totalEvents := buckets[negInfBucketSLO]
		if totalEvents != 0 {
			acceptedEvents := buckets[bucketSLO]
			perc := (float64(acceptedEvents) / float64(totalEvents)) * 100
			if perc < percentileSLO {
				return errors.NewMetricViolationError(
					metricName,
					fmt.Sprintf("Updates for %ss latency is within %d%%, expected %d%%, buckets:\n%v",
						bucketSLO,
						int(math.Floor(perc)),
						int(math.Floor(percentileSLO)),
						buckets,
					),
				)
			}
		}
	}
	return nil
}

func (g *negLatencyGatherer) gatherMetrics(executor QueryExecutor, startTime, endTime time.Time) negLatencyMetricMap {
	result := make(negLatencyMetricMap)
	gatherAndProcessSamples := func(metricName, queryTpl string, queryParams ...interface{}) {
		var samples []*model.Sample
		prevQueryTime := startTime
		currQueryTime := startTime.Add(negQueryInterval)

		for {
			if currQueryTime.After(endTime) {
				currQueryTime = endTime
			}
			interval := currQueryTime.Sub(prevQueryTime)
			promDuration := measurementutil.ToPrometheusTime(interval)
			query := fmt.Sprintf(queryTpl, append(queryParams, promDuration)...)
			newSamples, err := executor.Query(query, currQueryTime)
			if err == nil {
				samples = append(samples, newSamples...)
			} else {
				klog.V(2).Infof("Got error querying Prometheus: %v", err)
			}
			if currQueryTime == endTime {
				break
			}
			prevQueryTime = currQueryTime
			currQueryTime = currQueryTime.Add(queryInterval)
		}

		for _, sample := range samples {
			bucket, value := string(sample.Metric["le"]), int(math.Round(float64(sample.Value)))
			if _, ok := result[metricName]; !ok {
				result[metricName] = make(map[string]int)
			}
			result[metricName][bucket] = value
		}
	}

	gatherAndProcessSamples(negInitializationMetricName, negInitLatencyQuery)
	gatherAndProcessSamples(negAttachOpMetricName, negOpLatencyQuery, negAttachOpKey)
	gatherAndProcessSamples(negDetachOpMetricName, negOpLatencyQuery, negDetachOpKey)
	return result
}

func (g *negLatencyGatherer) Configure(_ *measurement.Config) error {
	return nil
}

func (g *negLatencyGatherer) IsEnabled(_ *measurement.Config) bool {
	return true
}

func (*negLatencyGatherer) String() string {
	return negLatencyMeasurementName
}
