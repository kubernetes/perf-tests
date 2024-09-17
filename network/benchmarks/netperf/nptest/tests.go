package main

import (
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

type testcase struct {
	TestParams
	Label           string
	Finished        bool
	BandwidthParser func(string) string
	JsonParser      func(string) string
	// Deprecated: We will use declarative approach to define test cases
	Type TestType
}

type TestParams struct {
	SourceNode      string
	DestinationNode string
	ClusterIP       bool
	MSS             int
	MsgSize         int
}

var testcases = []*testcase{
	// {
	// 	Label: "1 qperf TCP. Same VM using Pod IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       false,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            qperfTCPTest,
	// },
	// {
	// 	Label: "2 qperf TCP. Same VM using Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            qperfTCPTest,
	// },
	// {
	// 	Label: "3 qperf TCP. Remote VM using Pod IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w3",
	// 		ClusterIP:       false,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            qperfTCPTest,
	// },
	// {
	// 	Label: "4 qperf TCP. Remote VM using Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w3",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            qperfTCPTest,
	// },
	// {
	// 	Label: "5 qperf TCP. Hairpin Pod to own Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w2",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            qperfTCPTest,
	// },

	{
		Label: "6 iperf TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "7 iperf TCP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "8 iperf TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "9 iperf TCP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},
	{
		Label: "10 iperf TCP. Hairpin Pod to own Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w2",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		BandwidthParser: parsers.ParseIperfTCPBandwidth,
		Type:            iperfTCPTest,
	},

	// {
	// 	Label: "11 iperf SCTP. Same VM using Pod IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       false,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            iperfSctpTest,
	// },
	// {
	// 	Label: "12 iperf SCTP. Same VM using Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            iperfSctpTest,
	// },
	// {
	// 	Label: "13 iperf SCTP. Remote VM using Pod IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w1",
	// 		DestinationNode: "netperf-w3",
	// 		ClusterIP:       false,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            iperfSctpTest,
	// },
	// {
	// 	Label: "14 iperf SCTP. Remote VM using Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w3",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            iperfSctpTest,
	// },
	// {
	// 	Label: "15 iperf SCTP. Hairpin Pod to own Virtual IP",
	// 	TestParams: TestParams{
	// 		SourceNode:      "netperf-w2",
	// 		DestinationNode: "netperf-w2",
	// 		ClusterIP:       true,
	// 		MSS:             mssMin,
	// 	},
	// 	BandwidthParser: parsers.ParseIperfTCPBandwidth,
	// 	Type:            iperfSctpTest,
	// },

	{
		Label: "16 iperf UDP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		Type:            iperfUDPTest,
	},
	{
		Label: "17 iperf UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		Type:            iperfUDPTest,
	},
	{
		Label: "18 iperf UDP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		BandwidthParser: parsers.ParseIperfUDPBandwidth,
		Type:            iperfUDPTest,
	},
	{
		Label: "19 netperf. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},

	{
		Label: "20 netperf. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
		},
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "21 netperf. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "22 netperf. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
		},
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},
	{
		Label: "23 netperf. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		BandwidthParser: parsers.ParseNetperfBandwidth,
		Type:            netperfTest,
	},

	{
		Label: "24 iperf Throughput TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
		},
		JsonParser: parsers.ParseIperfThrouputTCPTest,
		Type:       iperfThroughputTest,
	},
	{
		Label: "25 iperf Throughput TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
		},
		JsonParser: parsers.ParseIperfThrouputTCPTest,
		Type:       iperfThroughputTest,
	},
	{
		Label: "26 iperf Throughput UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
		},
		JsonParser: parsers.ParseIperfThrouputUDPTest,
		Type:       iperfThroughputUDPTest,
	},
	{
		Label: "27 iperf Throughput UDP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
		},
		JsonParser: parsers.ParseIperfThrouputUDPTest,
		Type:       iperfThroughputUDPTest,
	},
}
