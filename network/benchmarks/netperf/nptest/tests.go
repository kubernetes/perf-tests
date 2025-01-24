/*
Copyright 2025 The Kubernetes Authors.

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
	"fmt"
	"strconv"
	"time"

	"k8s.io/perf-tests/network/nptest/parsers"
)

type TestType int

const (
	iperfTCPTest TestType = iota
	qperfTCPTest
	iperfUDPTest
	iperfSctpTest
	netperfTest
	iperfThroughputTest
	iperfThroughputUDPTest
)

type TestCase struct {
	TestParams
	Label           string
	Finished        bool
	BandwidthParser func(string) (float64, int)
	JSONParser      func(string) string
	TestRunner      func(ClientWorkItem) string
	// Deprecated: We will use declarative approach to define test cases
	Type TestType
}

type TestParams struct {
	SourceNode      string
	DestinationNode string
	ClusterIP       bool
	MSS             int
	MsgSize         int
	TestDuration    time.Duration
	// Fixed bandwidth for the test
	Bandwidth string
}

var testcases = []*TestCase{
	{
		Label: "0 iperf TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		TestRunner:      iperfBasicTCPTestRunner,
		JSONParser:      parsers.ParseIperfTCPResults,
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "1 iperf TCP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		TestRunner:      iperfBasicTCPTestRunner,
		JSONParser:      parsers.ParseIperfTCPResults,
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "2 iperf TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		TestRunner:      iperfBasicTCPTestRunner,
		JSONParser:      parsers.ParseIperfTCPResults,
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "3 iperf TCP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		TestRunner:      iperfBasicTCPTestRunner,
		JSONParser:      parsers.ParseIperfTCPResults,
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "4 iperf TCP. Hairpin Pod to own Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w2",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		TestRunner:      iperfBasicTCPTestRunner,
		JSONParser:      parsers.ParseIperfTCPResults,
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "5 iperf UDP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		TestRunner:      iperfBasicUDPTestRunner,
		JSONParser:      parsers.ParseIperfUDPResults,
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		Type:            iperfUDPTest,
	},
	{
		Label: "6 iperf UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		TestRunner:      iperfBasicUDPTestRunner,
		JSONParser:      parsers.ParseIperfUDPResults,
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		Type:            iperfUDPTest,
	},
	{
		Label: "7 iperf UDP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		TestRunner:      iperfBasicUDPTestRunner,
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		JSONParser:      parsers.ParseIperfUDPResults,
		Type:            iperfUDPTest,
	},
	{
		Label: "8 Iperf UDP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		TestRunner:      iperfBasicUDPTestRunner,
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "9 netperf. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
		},
		TestRunner:      netperfTestRunner,
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "10 netperf. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		TestRunner:      netperfTestRunner,
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "11 netperf. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
		},
		TestRunner:      netperfTestRunner,
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "12 netperf. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		TestRunner:      netperfTestRunner,
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "13 iperf Throughput TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
			Bandwidth:       "1G",
		},
		TestRunner: iperfThroughputTCPRunner,
		JSONParser: parsers.ParseIperfTCPResults,
		Type:       iperfThroughputTest,
	},
	{
		Label: "14 iperf Throughput TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
			Bandwidth:       "1G",
		},
		TestRunner: iperfThroughputTCPRunner,
		JSONParser: parsers.ParseIperfTCPResults,
		Type:       iperfThroughputTest,
	},
	{
		Label: "15 iperf Throughput UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
			Bandwidth:       "1G",
		},
		TestRunner: iperfThroughputUDPRunner,
		JSONParser: parsers.ParseIperfUDPResults,
		Type:       iperfThroughputUDPTest,
	},
	{
		Label: "16 iperf Throughput UDP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
			Bandwidth:       "1G",
		},
		TestRunner: iperfThroughputUDPRunner,
		JSONParser: parsers.ParseIperfUDPResults,
		Type:       iperfThroughputUDPTest,
	},
}

func iperfBasicTCPTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-N", "-i", "30", "-t", "10", "-f", "m", "-w", "512M", "-Z", "-J", "-P", parallelStreams, "-M", strconv.Itoa(w.Params.MSS)}, 15)
	return output
}

func iperfBasicUDPTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-i", "30", "-t", "10", "-f", "m", "-b", "0", "-u", "-J"}, 15)
	return output
}

func netperfTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(netperfPath, []string{netperfPath, "-H", w.Host}, 15)
	return output
}

func iperfThroughputTCPRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-J", "--time", fmt.Sprintf("%f", w.Params.TestDuration.Seconds()), "--bandwidth", w.Params.Bandwidth, "-w", "410K", "-P", "1"}, 15)
	return output
}

func iperfThroughputUDPRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-J", "--time", fmt.Sprintf("%f", w.Params.TestDuration.Seconds()), "--bandwidth", w.Params.Bandwidth, "-w", "410K", "-P", "1", "-u"}, 15)
	return output
}
