/*
Copyright 2016 The Kubernetes Authors.

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

/*
 launch.go

 Launch the netperf tests

 1. Launch the netperf-orch service
 2. Launch the worker pods
 3. Wait for the output csv data to show up in orchestrator pod logs
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	api "k8s.io/api/core/v1"
	"k8s.io/perf-tests/network/benchmarks/netperf/lib"
)

const (
	csvDataMarker     = "GENERATING CSV OUTPUT"
	csvEndDataMarker  = "END CSV DATA"
	jsonDataMarker    = "GENERATING JSON OUTPUT"
	jsonEndDataMarker = "END JSON OUTPUT"
	runUUID           = "latest"
	orchestratorPort  = 5202
	iperf3Port        = 5201
	qperf19766        = 19766
	qperf19765        = 19765
	netperfPort       = 12865
)

var (
	iterations    int
	tag           string
	kubeConfig    string
	testNamespace string
	netperfImage  string
	cleanupOnly   bool

	primaryNode   api.Node
	secondaryNode api.Node

	testFrom, testTo int

	jsonOutput bool
)

func init() {
	flag.IntVar(&iterations, "iterations", 1,
		"Number of iterations to run")
	flag.StringVar(&tag, "tag", runUUID, "Result file suffix")
	flag.StringVar(&netperfImage, "image", "sirot/netperf-latest", "Docker image used to run the network tests")
	flag.StringVar(&testNamespace, "namespace", "netperf", "Test namespace to run netperf pods")
	defaultKubeConfig := fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	flag.StringVar(&kubeConfig, "kubeConfig", defaultKubeConfig,
		"Location of the kube configuration file ($HOME/.kube/config")
	flag.BoolVar(&cleanupOnly, "cleanup", false,
		"(boolean) Run the cleanup resources phase only (use this flag to clean up orphaned resources from a test run)")
	flag.IntVar(&testFrom, "testFrom", 0, "start from test number testFrom")
	flag.IntVar(&testTo, "testTo", 5, "end at test number testTo")
	flag.BoolVar(&jsonOutput, "json", false, "Output JSON data along with CSV data")
}

func main() {
	flag.Parse()
	fmt.Println("Network Performance Test")
	fmt.Println("Parameters :")
	fmt.Println("Iterations      : ", iterations)
	fmt.Println("Test Namespace  : ", testNamespace)
	fmt.Println("Docker image    : ", netperfImage)
	fmt.Println("------------------------------------------------------------")

	testParams := lib.TestParams{
		Iterations:    iterations,
		Tag:           tag,
		TestNamespace: testNamespace,
		Image:         netperfImage,
		CleanupOnly:   cleanupOnly,
		TestFrom:      testFrom,
		TestTo:        testTo,
		JsonOutput:    jsonOutput,
		KubeConfig:    kubeConfig,
	}
	results, err := lib.PerformTests(testParams)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Results :")
	for _, result := range results {
		fmt.Println("CSV Result File  : ", result.CsvResultFile)
		fmt.Println("JSON Result File : ", result.JsonResultFile)
	}
}
