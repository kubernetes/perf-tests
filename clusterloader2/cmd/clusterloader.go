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
	"path"
	"time"

	"github.com/golang/glog"
	ginkgoconfig "github.com/onsi/ginkgo/config"
	ginkgoreporters "github.com/onsi/ginkgo/reporters"
	ginkgotypes "github.com/onsi/ginkgo/types"
	"github.com/spf13/pflag"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/bundle"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/simple"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/slos"
)

const (
	dashLine = "--------------------------------------------------------------------------------"
)

var (
	clusterLoaderConfig config.ClusterLoaderConfig
	testConfigPaths     []string
)

func initClusterFlags() {
	pflag.StringVar(&clusterLoaderConfig.ClusterConfig.KubeConfigPath, "kubeconfig", "", "Path to the kubeconfig file")
	pflag.IntVar(&clusterLoaderConfig.ClusterConfig.Nodes, "nodes", 0, "number of nodes")
	pflag.StringVar(&clusterLoaderConfig.ClusterConfig.Provider, "provider", "", "Cluster provider")
}

func validateClusterFlags() *errors.ErrorList {
	errList := errors.NewErrorList()
	if clusterLoaderConfig.ClusterConfig.KubeConfigPath == "" {
		errList.Append(fmt.Errorf("no kubeconfig path specified"))
	}
	return errList
}

func initFlags() {
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.StringVar(&clusterLoaderConfig.ReportDir, "report-dir", "", "Path to the directory where the reports should be saved. Default is empty, which cause reports being written to standard output.")
	pflag.StringArrayVar(&testConfigPaths, "testconfig", []string{}, "Paths to the test config files")
	pflag.StringVar(&clusterLoaderConfig.TestOverridesPath, "testoverrides", "", "Path to the config overrides file")
	initClusterFlags()
}

func validateFlags() *errors.ErrorList {
	errList := errors.NewErrorList()
	if len(testConfigPaths) < 1 {
		errList.Append(fmt.Errorf("no test config path specified"))
	}
	errList.Concat(validateClusterFlags())
	return errList
}

func completeConfig(f *framework.Framework) error {
	if clusterLoaderConfig.ClusterConfig.Nodes == 0 {
		nodes, err := util.GetSchedulableUntainedNodesNumber(f.GetClientSet())
		if err != nil {
			return fmt.Errorf("getting number of nodes error: %v", err)
		}
		clusterLoaderConfig.ClusterConfig.Nodes = nodes
		glog.Infof("ClusterConfig.Nodes set to %v", nodes)
	}
	if clusterLoaderConfig.ClusterConfig.MasterName == "" {
		masterName, err := util.GetMasterName(f.GetClientSet())
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterName = masterName
			glog.Infof("ClusterConfig.MasterName set to %v", masterName)
		} else {
			glog.Errorf("Getting master name error: %v", err)
		}
	}
	if clusterLoaderConfig.ClusterConfig.MasterIP == "" {
		masterIP, err := util.GetMasterExternalIP(f.GetClientSet())
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterIP = masterIP
			glog.Infof("ClusterConfig.MasterIP set to %v", masterIP)
		} else {
			glog.Errorf("Getting master ip error: %v", err)
		}
	}
	return nil
}

func createReportDir() error {
	if clusterLoaderConfig.ReportDir != "" {
		if _, err := os.Stat(clusterLoaderConfig.ReportDir); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err = os.Mkdir(clusterLoaderConfig.ReportDir, 0755); err != nil {
				return fmt.Errorf("report directory creation error: %v", err)
			}
		}
	}
	return nil
}

func printTestStart(name string) {
	glog.Infof(dashLine)
	glog.Infof("Running %v", name)
	glog.Infof(dashLine)
}

func printTestResult(name, status, errors string) {
	logf := glog.Infof
	if errors != "" {
		logf = glog.Errorf
	}
	logf(dashLine)
	logf("Test Finished")
	logf("  Test: %v", name)
	logf("  Status: %v", status)
	if errors != "" {
		logf("  Errors: %v", errors)
	}
	logf(dashLine)
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

	if err = completeConfig(f); err != nil {
		glog.Fatalf("Config completing error: %v", err)
	}

	if err = createReportDir(); err != nil {
		glog.Fatalf("Cannot create report directory: %v", err)
	}

	if err = util.LogClusterNodes(f.GetClientSet()); err != nil {
		glog.Errorf("Nodes info logging error: %v", err)
	}

	suiteSummary := &ginkgotypes.SuiteSummary{
		SuiteDescription:           "ClusterLoaderV2",
		NumberOfSpecsThatWillBeRun: len(testConfigPaths),
	}
	junitReporter := ginkgoreporters.NewJUnitReporter(path.Join(clusterLoaderConfig.ReportDir, "junit.xml"))
	junitReporter.SpecSuiteWillBegin(ginkgoconfig.GinkgoConfig, suiteSummary)
	testsStart := time.Now()
	for _, clusterLoaderConfig.TestConfigPath = range testConfigPaths {
		testStart := time.Now()
		specSummary := &ginkgotypes.SpecSummary{
			ComponentTexts: []string{suiteSummary.SuiteDescription, clusterLoaderConfig.TestConfigPath},
		}
		printTestStart(clusterLoaderConfig.TestConfigPath)
		if errList := test.RunTest(f, &clusterLoaderConfig); !errList.IsEmpty() {
			suiteSummary.NumberOfFailedSpecs++
			specSummary.State = ginkgotypes.SpecStateFailed
			specSummary.Failure = ginkgotypes.SpecFailure{
				Message: errList.String(),
			}
			printTestResult(clusterLoaderConfig.TestConfigPath, "Fail", errList.String())
		} else {
			specSummary.State = ginkgotypes.SpecStatePassed
			printTestResult(clusterLoaderConfig.TestConfigPath, "Success", "")
		}
		specSummary.RunTime = time.Since(testStart)
		junitReporter.SpecDidComplete(specSummary)
	}
	suiteSummary.RunTime = time.Since(testsStart)
	junitReporter.SpecSuiteDidEnd(suiteSummary)
	if suiteSummary.NumberOfFailedSpecs > 0 {
		glog.Fatalf("%d tests have failed!", suiteSummary.NumberOfFailedSpecs)
	}
}
