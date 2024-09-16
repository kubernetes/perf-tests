package main

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
	Label    string
	Finished bool
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
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w2",
	// 	Type: qperfTCPTest,
	// 	ClusterIP: false,
	// 	MsgSize: msgSizeMin,
	// },
	// {
	// 	Label: "2 qperf TCP. Same VM using Virtual IP",
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w2",
	// 	Type: qperfTCPTest,
	// 	ClusterIP: true,
	// 	MsgSize: msgSizeMin,
	// },
	// {
	// 	Label: "3 qperf TCP. Remote VM using Pod IP",
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w3",
	// 	Type: qperfTCPTest,
	// 	ClusterIP: false,
	// 	MsgSize: msgSizeMin,
	// },
	// {
	// 	Label: "4 qperf TCP. Remote VM using Virtual IP",
	// 	SourceNode: "netperf-w3",
	// 	DestinationNode: "netperf-w2",
	// 	Type: qperfTCPTest,
	// 	ClusterIP: true,
	// 	MsgSize: msgSizeMin,
	// },
	// {
	// 	Label: "5 qperf TCP. Hairpin Pod to own Virtual IP",
	// 	SourceNode: "netperf-w2",
	// 	DestinationNode: "netperf-w2",
	// 	Type: qperfTCPTest,
	// 	ClusterIP: true,
	// 	MsgSize: msgSizeMin,
	// },

	{
		Label: "1 iperf TCP. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		Type: iperfTCPTest,
	},
	{
		Label: "2 iperf TCP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		Type: iperfTCPTest,
	},
	{
		Label: "3 iperf TCP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMin,
		},
		Type: iperfTCPTest,
	},
	{
		Label: "4 iperf TCP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		Type: iperfTCPTest,
	},
	{
		Label: "5 iperf TCP. Hairpin Pod to own Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w2",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMin,
		},
		Type: iperfTCPTest,
	},

	// {
	// 	Label: "6 iperf SCTP. Same VM using Pod IP",
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w2",
	// 	Type: iperfSctpTest,
	// 	ClusterIP: false,
	// 	MSS: mssMin,
	// },
	// {
	// 	Label: "7 iperf SCTP. Same VM using Virtual IP",
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w2",
	// 	Type: iperfSctpTest,
	// 	ClusterIP: true,
	// 	MSS: mssMin,
	// },
	// {
	// 	Label: "8 iperf SCTP. Remote VM using Pod IP",
	// 	SourceNode: "netperf-w1",
	// 	DestinationNode: "netperf-w3",
	// 	Type: iperfSctpTest,
	// 	ClusterIP: false,
	// 	MSS: mssMin,
	// },
	// {
	// 	Label: "9 iperf SCTP. Remote VM using Virtual IP",
	// 	SourceNode: "netperf-w3",
	// 	DestinationNode: "netperf-w2",
	// 	Type: iperfSctpTest,
	// 	ClusterIP: true,
	// 	MSS: mssMin,
	// },
	// {
	// 	Label: "10 iperf SCTP. Hairpin Pod to own Virtual IP",
	// 	SourceNode: "netperf-w2",
	// 	DestinationNode: "netperf-w2",
	// 	Type: iperfSctpTest,
	// 	ClusterIP: true,
	// 	MSS: mssMin,
	// },

	{
		Label: "11 iperf UDP. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		Type: iperfUDPTest,
	},
	{
		Label: "12 iperf UDP. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		Type: iperfUDPTest,
	},
	{
		Label: "13 iperf UDP. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
			MSS:             mssMax,
		},
		Type: iperfUDPTest,
	},
	{
		Label: "14 netperf. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
			MSS:             mssMax,
		},
		Type: iperfUDPTest,
	},

	{
		Label: "15 netperf. Same VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       false,
		},
		Type: netperfTest,
	},
	{
		Label: "16 netperf. Same VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		Type: netperfTest,
	},
	{
		Label: "17 netperf. Remote VM using Pod IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w1",
			DestinationNode: "netperf-w3",
			ClusterIP:       false,
		},
		Type: netperfTest,
	},
	{
		Label: "18 netperf. Remote VM using Virtual IP",
		TestParams: TestParams{
			SourceNode:      "netperf-w3",
			DestinationNode: "netperf-w2",
			ClusterIP:       true,
		},
		Type: netperfTest,
	},
}
