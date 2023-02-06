/*
Copyright 2023 The Kubernetes Authors.

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
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"
	client "k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pod-creation-reachability-latency/test-client"
)

func main() {
	klog.Infof("Starting pod creation reachability latency client")
	defer klog.Infof("Closing pod creation reachability latency client")

	mainStopChan := make(chan os.Signal, 1)
	testClient, err := client.NewTestClient(mainStopChan)
	if err != nil {
		klog.Fatalf("Failed to created the test client, error: %v", err)
	}

	signal.Notify(testClient.MainStopChan, syscall.SIGTERM)
	testClient.Run()
}
