/*
Copyright 2019 The Kubernetes Authors.

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

import "time"

//net-rpc service listen ports
const (
	ControllerRpcSvcPort = "5002"
	WorkerRpcSvcPort     = "5003"
	HttpPort             = "5301"
)

//Environment variables
const (
	PodName   = "POD_NAME"
	NodeName  = "NODE_NAME"
	PodIP     = "POD_IP"
	ClusterIp = "CLUSTER_IP"
)

const (
	WorkerMode     = "worker"
	ControllerMode = "controller"
)

const (
	Protocol_TCP  = "TCP"
	Protocol_UDP  = "UDP"
	Protocol_HTTP = "HTTP"
)

//TCP result array Index mapping
const (
	TCPTransfer = iota
	TCPBW
)

//UDP result array Index mapping
const (
	UDPTransfer = iota
	UDPBW
	UDPJitter
	UDPLostPkt
	UDPTotalPkt
	UDPLatPer
	UDPLatAvg
	UDPLatMin
	UDPLatMax
	UDPLatStdD
	UDPPps
)

//HTTP result array Index mapping
const (
	HTTPTxs = iota
	HTTPAvl
	HTTPTimeElps
	HTTPDataTrsfd
	HTTPRespTime
	HTTPTxRate
	HTTPThroughput
	HTTPConcurrency
	HTTPTxSuccesful
	HTTPFailedTxs
	HTTPLongestTx
	HTTPShortestTx
)

const RatioSeparator = ":"

const (
	networkPerfMetricsName      = "NetworkPerformanceMetrics"
	netperfNamespace            = "netperf-1"
	checkWorkerPodReadyInterval = 1 * time.Second
	workerLabel                 = "worker"
)

type WorkerPodData struct {
	PodName    string
	WorkerNode string
	PodIp      string
	ClusterIP  string
}

type WorkerPodRegReply struct {
	Response string
}

type ClientRequest struct {
	Duration      string
	Timestamp     int64 //epoch time
	DestinationIP string
}

type ServerRequest struct {
	Duration   string
	Timestamp  int64 //epoch time
	NumClients string
}

type WorkerResponse struct {
	PodName    string
	WorkerNode string
}

type UniquePodPair struct {
	SrcPodName    string
	SrcPodIp      string
	DestPodName   string
	DestPodIp     string
	IsLastPodPair bool `default: false`
}

type MetricRequest struct {
}

type MetricResponse struct {
	Result          []float64
	WorkerStartTime string
}
