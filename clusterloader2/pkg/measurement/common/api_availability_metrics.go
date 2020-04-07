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
	IP                         string        `json:"IP"`
	AvailabilityPercentage     float32       `json:"availabilityPercentage"`
	LongestUnavailableDuration time.Duration `json:"longestUnavailableDuration"`
}

type apiAvailabilityOutput struct {
	ClusterMetrics *apiAvailabilitySummary   `json:"clusterMetrics"`
	MasterMetrics  []*apiAvailabilitySummary `json:"masterMetrics"`
}

func (output apiAvailabilityOutput) createClusterSummary(metrics *apiAvailabilityMetrics, probeFrequency int) {
	metrics.buildAPIAvailabilityMetricsSummary(probeFrequency, true, "")
}

func (output apiAvailabilityOutput) createMastersSummary(metrics map[string]*apiAvailabilityMetrics, hosts []string, probeFrequency int) {
	for _, host := range hosts {
		masterHostAvailabilitySummary := metrics[host].buildAPIAvailabilityMetricsSummary(probeFrequency, false, host)
		output.MasterMetrics = append(output.MasterMetrics, masterHostAvailabilitySummary)
	}

}

// updateMaxConsecutiveFailuresIfNeeded checks if the recently concluded consecutive failed number of probes is
// higher than the max consecutive failed number of probes so far
// if yes, then Update max consecutive failed probes
func (a *apiAvailabilityMetrics) updateMaxConsecutiveFailuresIfNeeded() {
	if a.consecutiveFailedProbes > a.maxConsecutiveFailedProbes {
		a.maxConsecutiveFailedProbes = a.consecutiveFailedProbes
	}
}

func (a *apiAvailabilityMetrics) updateFailureMetrics() {
	a.numFailures++
	a.consecutiveFailedProbes++
	a.updateMaxConsecutiveFailuresIfNeeded()
}

func (a *apiAvailabilityMetrics) updateSuccessMetrics() {
	a.numSuccesses++
	a.consecutiveFailedProbes = 0
}

func (a *apiAvailabilityMeasurement) updateAvailabilityMetrics(apiServerAvailable bool, metrics *apiAvailabilityMetrics) {
	if apiServerAvailable {
		metrics.updateSuccessMetrics()
	} else {
		metrics.updateFailureMetrics()
	}
}

func (a *apiAvailabilityMetrics) buildAPIAvailabilityMetricsSummary(probeFrequency int, clusterMetrics bool, hostIP string) *apiAvailabilitySummary {
	// Gather availability a
	availabilityPercentage := (float32(a.numSuccesses) / float32(a.numSuccesses+a.numFailures)) * 100
	longestUnavailableDuration := time.Duration(a.maxConsecutiveFailedProbes * probeFrequency)

	apiAvailabilitySummary := &apiAvailabilitySummary{}
	apiAvailabilitySummary.AvailabilityPercentage = availabilityPercentage
	apiAvailabilitySummary.LongestUnavailableDuration = longestUnavailableDuration

	return apiAvailabilitySummary
}
