/*
Copyright 2018 The Kubernetes Authors.

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
	"fmt"
	"os"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
)

var (
	clusterLoaderConfig config.ClusterLoaderConfig
	testConfigPaths     []string
)

func initClusterFlags() {
	pflag.StringVar(&clusterLoaderConfig.ClusterConfig.KubeConfigPath, "kubeconfig", "", "Path to the kubeconfig file")
	// TODO(krzysied): query actual number of nodes with client (if not set).
	pflag.IntVar(&clusterLoaderConfig.ClusterConfig.Nodes, "nodes", 0, "number of nodes")
}

func validateClusterFlags() *util.ErrorList {
	errList := util.NewErrorList()
	if clusterLoaderConfig.ClusterConfig.KubeConfigPath == "" {
		errList.Append(fmt.Errorf("no kubeconfig path specified"))
	}
	if clusterLoaderConfig.ClusterConfig.Nodes < 1 {
		errList.Append(fmt.Errorf("number of nodes not specified"))
	}
	return errList
}

func initFlags() {
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.StringVar(&clusterLoaderConfig.ReportDir, "report-dir", "", "Path to the directory where the reports should be saved. Default is empty, which cause reports being written to standard output.")
	pflag.StringArrayVar(&testConfigPaths, "testconfig", []string{}, "Paths to the test config files")
	initClusterFlags()
}

func validateFlags() *util.ErrorList {
	errList := util.NewErrorList()
	if len(testConfigPaths) < 1 {
		errList.Append(fmt.Errorf("no test config path specified"))
	}
	errList.Concat(validateClusterFlags())
	return errList
}

func main() {
	defer glog.Flush()
	initFlags()
	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		glog.Fatalf("Flag parse failed: %v", err)
	}
	if errList := validateFlags(); !errList.IsEmpty() {
		glog.Fatalf("Parsing flags error: %v", errList.String())
	}

	f, err := framework.NewFramework(clusterLoaderConfig.ClusterConfig.KubeConfigPath)
	if err != nil {
		glog.Fatalf("Framework creation error: %v", err)
	}
	for _, clusterLoaderConfig.TestConfigPath = range testConfigPaths {
		glog.Infof("Running %v", clusterLoaderConfig.TestConfigPath)
		if errList := test.RunTest(f, &clusterLoaderConfig); !errList.IsEmpty() {
			glog.Fatalf("Test execution failed: %v", errList.String())
		}
		glog.Infof("Test %v ran successfully!", clusterLoaderConfig.TestConfigPath)
	}
}
