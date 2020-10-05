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

package api

//net-rpc service listen ports
const (
	ControllerRpcSvcPort = "5002"
	WorkerRpcSvcPort     = "5003"
	ControllerHost       = "controller"
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
	Protocol_TCP  = "tcp"
	Protocol_UDP  = "udp"
	Protocol_HTTP = "http"
)

const RatioSeparator = ":"

type WorkerPodData struct {
	PodName    string
	WorkerNode string
	PodIp      string
	ClusterIP  string
}

type WorkerPodRegReply struct {
	Response string
}

type WorkerRequest struct {
	Duration      int
	DestinationIP string
	Timestamp     string
}

type WorkerResponse struct {
	PodName    string
	WorkerNode string
}

type MetricRequest struct {
}

type MetricResponse struct {
}
