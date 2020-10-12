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

package main

import (
	"errors"
	"flag"
	"strings"
	"sync"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/pkg/controller"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/pkg/worker"
)

var (
	mode         = flag.String("mode", "", "Mode that should be run. Supported values: controller or worker")
	ratio        = flag.String("client-server-pod-ratio", "", "Client POD to Server POD ratio")
	duration     = flag.String("measurement-duration", "", "Duration of metric collection in seconds")
	protocol     = flag.String("protocol", "", "Protocol to be tested. Supported values: tcp or or udp or http")
	controllerIp = flag.String("controlerIP", "", "IP address of controller pod")
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(1)
	klog.Infof("Pod running in: %s mode %s ratio\n", *mode, *ratio)

	err := validate(*mode, *ratio, *protocol)
	if err != nil {
		klog.Fatalf("Validation failed with err : %s", err)
	}

	switch *mode {
	case api.ControllerMode:
		controller.Start(*ratio)
		controller.WaitForWorkerPodReg()
		controller.ExecuteTest(*ratio, *duration, *protocol)
	case api.WorkerMode:
		worker.Start(*controllerIp)
	default:
		klog.Fatalf("Unrecognized mode: %q", *mode)
	}

	wg.Wait()
}

func validate(mode string, ratio string, protocol string) error {

	if mode != api.ControllerMode && mode != api.WorkerMode {
		return errors.New("invalid mode")
	}

	if mode == api.WorkerMode && *controllerIp == "" {
		return errors.New("Controller hostname/ip not specified")
	} else {
		return nil
	}

	if !strings.Contains(ratio, api.RatioSeparator) {
		return errors.New("invalid ratio. : missing")
	}

	if protocol != api.Protocol_TCP && protocol != api.Protocol_UDP && protocol != api.Protocol_HTTP {
		return errors.New("invalid protocol")
	}
	return nil
}
