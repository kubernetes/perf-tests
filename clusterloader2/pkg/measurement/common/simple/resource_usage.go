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

package simple

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/gatherers"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	resourceUsageMetricName = "ResourceUsageSummary"
)

func init() {
	measurement.Register(resourceUsageMetricName, createResourceUsageMetricMeasurement)
}

func createResourceUsageMetricMeasurement() measurement.Measurement {
	return &resourceUsageMetricMeasurement{}
}

type resourceUsageMetricMeasurement struct {
	gatherer *gatherers.ContainerResourceGatherer
}

// Execute supports two actions:
// - start - Starts resource metrics collecting.
// - gather - Gathers and prints current resource usage metrics.
func (e *resourceUsageMetricMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return summaries, err
	}

	switch action {
	case "start":
		provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterConfig.Provider)
		if err != nil {
			return summaries, err
		}
		host, err := util.GetStringOrDefault(config.Params, "host", config.ClusterConfig.MasterIP)
		if err != nil {
			return summaries, err
		}
		nodeMode, err := util.GetStringOrDefault(config.Params, "nodeMode", "")
		if err != nil {
			return summaries, err
		}
		var nodesSet gatherers.NodesSet
		switch nodeMode {
		case "master":
			nodesSet = gatherers.MasterNodes
		case "masteranddns":
			nodesSet = gatherers.MasterAndDNSNodes
		default:
			nodesSet = gatherers.AllNodes
		}

		glog.Infof("%v: starting resource usage collecting...", resourceUsageMetricName)
		e.gatherer, err = gatherers.NewResourceUsageGatherer(config.ClientSet, provider, host, gatherers.ResourceGathererOptions{
			InKubemark:                  strings.ToLower(provider) == "kubemark",
			Nodes:                       nodesSet,
			ResourceDataGatheringPeriod: 60 * time.Second,
			ProbeDuration:               15 * time.Second,
			PrintVerboseLogs:            false,
		}, nil)
		if err != nil {
			return summaries, err
		}
		go e.gatherer.StartGatheringData()
		return summaries, nil
	case "gather":
		if e.gatherer == nil {
			glog.Infof("%s: gatherer not initialized", resourceUsageMetricName)
			return summaries, nil
		}
		// TODO(krzysied): Add suppport for resource constraints.
		glog.Infof("%v, gathering resource usage...", resourceUsageMetricName)
		summary, err := e.gatherer.StopAndSummarize([]int{90, 99, 100}, make(map[string]measurementutil.ResourceConstraint))
		if err != nil {
			return summaries, err
		}

		resourceSummary := resourceUsageSummary(*summary)
		summaries := append(summaries, &resourceSummary)
		return summaries, nil

	default:
		return summaries, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (e *resourceUsageMetricMeasurement) Dispose() {
	if e.gatherer != nil {
		e.gatherer.Dispose()
	}
}

type resourceUsageSummary gatherers.ResourceUsageSummary

// SummaryName returns name of the summary.
func (e *resourceUsageSummary) SummaryName() string {
	return resourceUsageMetricName
}

// PrintSummary returns summary as a string.
func (e *resourceUsageSummary) PrintSummary() (string, error) {
	return util.PrettyPrintJSON(e)
}
