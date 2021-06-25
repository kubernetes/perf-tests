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

package main

import (
	"flag"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/network/netperfbenchmark/pkg/worker"
)

var (
	extraArgumentsMap = map[string]*string{
		"client_UDP":  flag.String("udpClientExtraArguments", "", "Extra arguments for udp client command"),
		"server_UDP":  flag.String("udpServerExtraArguments", "", "Extra arguments for udp server command"),
		"client_TCP":  flag.String("tcpClientExtraArguments", "", "Extra arguments for tcp client command"),
		"server_TCP":  flag.String("tcpServerExtraArguments", "", "Extra arguments for tcp server command"),
		"client_HTTP": flag.String("httpClientExtraArguments", "", "Extra arguments for http client command"),
	}
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()
	worker.NewWorker().Start(extraArgumentsMap)
}
