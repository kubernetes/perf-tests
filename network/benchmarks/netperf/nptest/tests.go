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
	JsonParser      func(string) string
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
		TestRunner:      iperfBasicTcpTestRunner,
		JsonParser:      parsers.ParseIperfTcpResults,
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
		TestRunner:      iperfBasicTcpTestRunner,
		JsonParser:      parsers.ParseIperfTcpResults,
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
		TestRunner:      iperfBasicTcpTestRunner,
		JsonParser:      parsers.ParseIperfTcpResults,
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
		TestRunner:      iperfBasicTcpTestRunner,
		JsonParser:      parsers.ParseIperfTcpResults,
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
		TestRunner:      iperfBasicTcpTestRunner,
		JsonParser:      parsers.ParseIperfTcpResults,
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
		TestRunner:      iperfBasicUdpTestRunner,
		JsonParser:      parsers.ParseIperfUdpResults,
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
		TestRunner:      iperfBasicUdpTestRunner,
		JsonParser:      parsers.ParseIperfUdpResults,
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
		TestRunner:      iperfBasicUdpTestRunner,
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		JsonParser:      parsers.ParseIperfUdpResults,
		Type:            iperfUDPTest,
	},
	{
		Label: "8 Iperf Udp. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		TestRunner:      iperfBasicUdpTestRunner,
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
		Label: "13 iperf Default TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
		},
		TestRunner: defaultIperfTCPRunner,
		JsonParser: parsers.ParseIperfTcpResults,
		Type:       iperfThroughputTest,
	},
	{
		Label: "14 iperf Default TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
		},
		TestRunner: defaultIperfTCPRunner,
		JsonParser: parsers.ParseIperfTcpResults,
		Type:       iperfThroughputTest,
	},
	{
		Label: "15 iperf Default UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
		},
		TestRunner: defaultIperfUDPRunner,
		JsonParser: parsers.ParseIperfUdpResults,
		Type:       iperfThroughputUDPTest,
	},
	{
		Label: "16 iperf Default UDP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			TestDuration:    10 * time.Minute,
		},
		TestRunner: defaultIperfUDPRunner,
		JsonParser: parsers.ParseIperfUdpResults,
		Type:       iperfThroughputUDPTest,
	},
}

func iperfBasicTcpTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-N", "-i", "30", "-t", "10", "-f", "m", "-w", "512M", "-Z", "-J", "-P", parallelStreams, "-M", strconv.Itoa(w.Params.MSS)}, 15)
	return output
}

func iperfBasicUdpTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-i", "30", "-t", "10", "-f", "m", "-b", "0", "-u", "-J"}, 15)
	return output
}

func netperfTestRunner(w ClientWorkItem) string {
	output, _ := cmdExec(netperfPath, []string{netperfPath, "-H", w.Host}, 15)
	return output
}

// TODO: Implement a common pattern to run iperf3 command and re utilize them
func defaultIperfTCPRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-J", "--time", fmt.Sprintf("%f", w.Params.TestDuration.Seconds())}, 15)
	return output
}

func defaultIperfUDPRunner(w ClientWorkItem) string {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-c", w.Host, "-V", "-J", "--time", fmt.Sprintf("%f", w.Params.TestDuration.Seconds()), "-u", "-b", "0"}, 15)
	return output
}
