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

	"github.com/golang/glog"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/errors"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/test"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
)

var (
	testConfigPath = flag.String("testconfig", "", "path to the test config file")
)

func validateFlags() []error {
	errList := make([]error, 0)
	if k8sframework.TestContext.KubeConfig == "" {
		errList = append(errList, fmt.Errorf("no kubeconfig path specified"))
	}
	if *testConfigPath == "" {
		errList = append(errList, fmt.Errorf("no test config path specified"))
	}
	return errList
}

func main() {
	defer glog.Flush()
	gomega.RegisterFailHandler(failHandler)
	k8sframework.RegisterCommonFlags()
	k8sframework.RegisterClusterFlags()
	k8sframework.RegisterStorageFlags()
	flag.Parse()
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	if errList := validateFlags(); len(errList) > 0 {
		glog.Fatalf("Parsing flags error: %v", errors.NewAggregate(errList).Error())
	}

	f, err := framework.NewFramework(k8sframework.TestContext.KubeConfig)
	if err != nil {
		glog.Fatalf("Framework creation error: %v", err)
	}
	c, err := config.ReadConfig(*testConfigPath)
	if err != nil {
		glog.Fatalf("Config reading error: %v", err)
	}

	if errList := test.RunTest(f, c); len(errList) > 0 {
		glog.Fatalf("Test execution failed: %v", errors.NewAggregate(errList).Error())
	}

	glog.Info("Test ran successfully!")
}

func failHandler(message string, callerSkip ...int) {
	glog.Errorf("panic: %v", message)
}
