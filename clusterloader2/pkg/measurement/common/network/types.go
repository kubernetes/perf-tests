/*
Copyright 2021 The Kubernetes Authors.

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

package network

import (
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

const (
	protocolTCP  = "TCP"
	protocolUDP  = "UDP"
	protocolHTTP = "HTTP"
)

// TCP result array Index mapping
// Other unused metrics are not included
const (
	tcpBandwidth = 1
)

// UDP result array Index mapping
// Other unused metrics are not included
const (
	udpJitter                = 2
	udpLostPacketsPercentage = 5
	udpLatencyAverage        = 6
	udpPacketPerSecond       = 10
)

// HTTP result array Index mapping
// Other unused metrics are not included
const (
	httpResponseTime = 4
)

// Client-To-Server Pod ratio indicator
const (
	oneToOne   = "1:1"
	manyToOne  = "N:1"
	manyToMany = "N:M"
	invalid    = "invalid"
)

const (
	perc05 = "Perc05"
	perc50 = "Perc50"
	perc95 = "Perc95"
	value  = "Value"
)

const (
	throughput      = "Throughput"
	latency         = "Latency"
	jitter          = "Jitter"
	lostPackets     = "Lost_Packets"
	packetPerSecond = "Packet_Per_Second"
	responseTime    = "Response_Time"
)

var metricUnitMap = map[string]string{
	throughput:      "kbytes/sec",
	latency:         "ms",
	jitter:          "ms",
	lostPackets:     "percentage",
	packetPerSecond: "pps",
	responseTime:    "seconds",
}

// metricResponse represents response for metric query from worker pod.
type metricResponse struct {
	Metrics     []float64
	Error       string
	WorkerDelay float64
}

// testResultSummary consists of metrics results and testcase details.
type testResultSummary struct {
	podRatio  string
	protocol  string
	service   string
	DataItems []measurementutil.DataItem
}
