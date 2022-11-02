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

package common

import (
	"time"
)

type apiAvailabilityMetrics struct {
	numSuccesses               int
	numFailures                int
	maxConsecutiveFailedProbes int
	consecutiveFailedProbes    int
}

type apiAvailabilitySummary struct {
	IP                       string  `json:"IP,omitempty"`
	AvailabilityPercentage   float64 `json:"availabilityPercentage"`
	LongestUnavailablePeriod string  `json:"longestUnavailablePeriod"`
}

type apiAvailabilityOutput struct {
	ClusterSummary apiAvailabilitySummary   `json:"clusterMetrics"`
	HostSummaries  []apiAvailabilitySummary `json:"hostMetrics"`
}

func (a *apiAvailabilityMetrics) update(availability bool) {
	if availability {
		a.numSuccesses++
		a.consecutiveFailedProbes = 0
		return
	}
	a.numFailures++
	a.consecutiveFailedProbes++
	if a.consecutiveFailedProbes > a.maxConsecutiveFailedProbes {
		a.maxConsecutiveFailedProbes = a.consecutiveFailedProbes
	}
}

func (a *apiAvailabilityMetrics) buildSummary(pollFrequency time.Duration, hostIP string) apiAvailabilitySummary {
	availabilityPercentage := float64(100)
	if a.numSuccesses > 0 || a.numFailures > 0 {
		availabilityPercentage = (float64(a.numSuccesses) / float64(a.numSuccesses+a.numFailures)) * 100
	}
	longestUnavailablePeriod := time.Duration(a.maxConsecutiveFailedProbes) * pollFrequency
	return apiAvailabilitySummary{
		AvailabilityPercentage:   availabilityPercentage,
		LongestUnavailablePeriod: longestUnavailablePeriod.String(),
		IP:                       hostIP,
	}
}

func createHostSummary(metrics map[string]*apiAvailabilityMetrics, hostIPs []string, pollFrequency time.Duration) []apiAvailabilitySummary {
	summaries := []apiAvailabilitySummary{}
	for _, ip := range hostIPs {
		hostSummary := metrics[ip].buildSummary(pollFrequency, ip)
		summaries = append(summaries, hostSummary)
	}
	return summaries
}

func createClusterSummary(metrics *apiAvailabilityMetrics, pollFrequency time.Duration) apiAvailabilitySummary {
	return metrics.buildSummary(pollFrequency, "")
}
