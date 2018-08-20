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
	"path/filepath"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/test"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
)

var (
	clusterConfig  config.ClusterConfig
	testConfigPath string
)

func initClusterFlags() {
	flag.StringVar(&clusterConfig.KubeConfigPath, "kubeconfig", "", "Path to the kubeconfig file")
	flag.IntVar(&clusterConfig.NodeCount, "nodes", -1, "Number of nodes in the cluster")
}

func validateClusterFlags() []error {
	errList := make([]error, 0)
	if clusterConfig.KubeConfigPath == "" {
		errList = append(errList, fmt.Errorf("no kubeconfig path specified"))
	}
	return errList
}

func initFlags() {
	flag.StringVar(&testConfigPath, "testconfig", "", "Path to the test config file")
	initClusterFlags()
}

func validateFlags() []error {
	errList := make([]error, 0)
	if testConfigPath == "" {
		errList = append(errList, fmt.Errorf("no test config path specified"))
	}
	errList = append(errList, validateClusterFlags()...)
	return errList
}

func main() {
	defer glog.Flush()
	initFlags()
	flag.Parse()
	if errList := validateFlags(); len(errList) > 0 {
		glog.Fatalf("Parsing flags error: %v", errors.NewAggregate(errList).Error())
	}

	f, err := framework.NewFramework(clusterConfig.KubeConfigPath, filepath.Dir(testConfigPath))
	if err != nil {
		glog.Fatalf("Framework creation error: %v", err)
	}
	testConfig, err := config.ReadConfig(testConfigPath)
	if err != nil {
		glog.Fatalf("Config reading error: %v", err)
	}

	if errList := test.RunTest(f, &clusterConfig, testConfig); len(errList) > 0 {
		glog.Fatalf("Test execution failed: %v", errors.NewAggregate(errList).Error())
	}

	glog.Info("Test ran successfully!")
}
