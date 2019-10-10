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
	"fmt"
	"os"
	"path"
	"time"

	ginkgoconfig "github.com/onsi/ginkgo/config"
	ginkgoreporters "github.com/onsi/ginkgo/reporters"
	ginkgotypes "github.com/onsi/ginkgo/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/prometheus"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/bundle"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/probes"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/simple"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/slos"
)

const (
	dashLine        = "--------------------------------------------------------------------------------"
	nodesPerClients = 100
)

var (
	clusterLoaderConfig config.ClusterLoaderConfig
	testConfigPaths     []string
)

func initClusterFlags() {
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.KubeConfigPath, "kubeconfig", "KUBECONFIG", "", "Path to the kubeconfig file")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.Nodes, "nodes", "NUM_NODES", 0, "number of nodes")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.Provider, "provider", "PROVIDER", "", "Cluster provider")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.MasterName, "mastername", "MASTER_NAME", "", "Name of the masternode")
	// TODO(#595): Change the name of the MASTER_IP and MASTER_INTERNAL_IP flags and vars to plural
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterIPs, "masterip", "MASTER_IP", nil /*defaultValue*/, "Hostname/IP of the master node, supports multiple values when separated by commas")
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterInternalIPs, "master-internal-ip", "MASTER_INTERNAL_IP", nil /*defaultValue*/, "Cluster internal/private IP of the master vm, supports multiple values when separated by commas")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.KubemarkRootKubeConfigPath, "kubemark-root-kubeconfig", "KUBEMARK_ROOT_KUBECONFIG", "",
		"Path the to kubemark root kubeconfig file, i.e. kubeconfig of the cluster where kubemark cluster is run. Ignored if provider != kubemark")
}

func validateClusterFlags() *errors.ErrorList {
	errList := errors.NewErrorList()
	if clusterLoaderConfig.ClusterConfig.KubeConfigPath == "" {
		errList.Append(fmt.Errorf("no kubeconfig path specified"))
	}
	if clusterLoaderConfig.ClusterConfig.Provider == "kubemark" &&
		clusterLoaderConfig.EnablePrometheusServer &&
		clusterLoaderConfig.ClusterConfig.KubemarkRootKubeConfigPath == "" {
		errList.Append(fmt.Errorf("no kubemark-root-kubeconfig path specified"))
	}
	return errList
}

func initFlags() {
	flags.StringVar(&clusterLoaderConfig.ReportDir, "report-dir", "", "Path to the directory where the reports should be saved. Default is empty, which cause reports being written to standard output.")
	flags.BoolEnvVar(&clusterLoaderConfig.EnablePrometheusServer, "enable-prometheus-server", "ENABLE_PROMETHEUS_SERVER", false, "Whether to set-up the prometheus server in the cluster.")
	flags.BoolEnvVar(&clusterLoaderConfig.TearDownPrometheusServer, "tear-down-prometheus-server", "TEAR_DOWN_PROMETHEUS_SERVER", true, "Whether to tear-down the prometheus server after tests (if set-up).")
	flags.StringArrayVar(&testConfigPaths, "testconfig", []string{}, "Paths to the test config files")
	flags.StringArrayVar(&clusterLoaderConfig.TestOverridesPath, "testoverrides", []string{}, "Paths to the config overrides file. The latter overrides take precedence over changes in former files.")
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

func completeConfig(m *framework.MultiClientSet) error {
	if clusterLoaderConfig.ClusterConfig.Nodes == 0 {
		nodes, err := util.GetSchedulableUntainedNodesNumber(m.GetClient())
		if err != nil {
			return fmt.Errorf("getting number of nodes error: %v", err)
		}
		clusterLoaderConfig.ClusterConfig.Nodes = nodes
		klog.Infof("ClusterConfig.Nodes set to %v", nodes)
	}
	if clusterLoaderConfig.ClusterConfig.MasterName == "" {
		masterName, err := util.GetMasterName(m.GetClient())
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterName = masterName
			klog.Infof("ClusterConfig.MasterName set to %v", masterName)
		} else {
			klog.Errorf("Getting master name error: %v", err)
		}
	}
	if len(clusterLoaderConfig.ClusterConfig.MasterIPs) == 0 {
		masterIPs, err := util.GetMasterIPs(m.GetClient(), corev1.NodeExternalIP)
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterIPs = masterIPs
			klog.Infof("ClusterConfig.MasterIP set to %v", masterIPs)
		} else {
			klog.Errorf("Getting master external ip error: %v", err)
		}
	}
	if len(clusterLoaderConfig.ClusterConfig.MasterInternalIPs) == 0 {
		masterIPs, err := util.GetMasterIPs(m.GetClient(), corev1.NodeInternalIP)
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterInternalIPs = masterIPs
			klog.Infof("ClusterConfig.MasterInternalIP set to %v", masterIPs)
		} else {
			klog.Errorf("Getting master internal ip error: %v", err)
		}
	}
	return nil
}

func verifyCluster(c kubernetes.Interface) error {
	numSchedulableNodes, err := util.GetSchedulableUntainedNodesNumber(c)
	if err != nil {
		return err
	}
	if numSchedulableNodes == 0 {
		return fmt.Errorf("no schedulable nodes in the cluster")
	}
	return nil
}

func getClientsNumber(nodesNumber int) int {
	return (nodesNumber + nodesPerClients - 1) / nodesPerClients
}

func createReportDir() error {
	if clusterLoaderConfig.ReportDir != "" {
		if _, err := os.Stat(clusterLoaderConfig.ReportDir); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err = os.MkdirAll(clusterLoaderConfig.ReportDir, 0755); err != nil {
				return fmt.Errorf("report directory creation error: %v", err)
			}
		}
	}
	return nil
}

func printTestStart(name string) {
	klog.Infof(dashLine)
	klog.Infof("Running %v", name)
	klog.Infof(dashLine)
}

func printTestResult(name, status, errors string) {
	logf := klog.Infof
	if errors != "" {
		logf = klog.Errorf
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
	defer klog.Flush()
	initFlags()
	if err := flags.Parse(); err != nil {
		klog.Exitf("Flag parse failed: %v", err)
	}
	if errList := validateFlags(); !errList.IsEmpty() {
		klog.Exitf("Parsing flags error: %v", errList.String())
	}

	mclient, err := framework.NewMultiClientSet(clusterLoaderConfig.ClusterConfig.KubeConfigPath, 1)
	if err != nil {
		klog.Exitf("Client creation error: %v", err)
	}

	if err = completeConfig(mclient); err != nil {
		klog.Exitf("Config completing error: %v", err)
	}

	klog.Infof("Using config: %+v", clusterLoaderConfig)

	if err = createReportDir(); err != nil {
		klog.Exitf("Cannot create report directory: %v", err)
	}

	if err = util.LogClusterNodes(mclient.GetClient()); err != nil {
		klog.Errorf("Nodes info logging error: %v", err)
	}

	if err = verifyCluster(mclient.GetClient()); err != nil {
		klog.Exitf("Cluster verification error: %v", err)
	}

	f, err := framework.NewFramework(
		&clusterLoaderConfig.ClusterConfig,
		getClientsNumber(clusterLoaderConfig.ClusterConfig.Nodes),
	)
	if err != nil {
		klog.Exitf("Framework creation error: %v", err)
	}

	var prometheusController *prometheus.PrometheusController
	var prometheusFramework *framework.Framework
	if clusterLoaderConfig.EnablePrometheusServer {
		if prometheusController, err = prometheus.NewPrometheusController(&clusterLoaderConfig); err != nil {
			klog.Exitf("Error while creating Prometheus Controller: %v", err)
		}
		prometheusFramework = prometheusController.GetFramework()
		if err := prometheusController.SetUpPrometheusStack(); err != nil {
			klog.Exitf("Error while setting up prometheus stack: %v", err)
		}
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
		if errList := test.RunTest(f, prometheusFramework, &clusterLoaderConfig); !errList.IsEmpty() {
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

	if clusterLoaderConfig.EnablePrometheusServer && clusterLoaderConfig.TearDownPrometheusServer {
		if err := prometheusController.TearDownPrometheusStack(); err != nil {
			klog.Errorf("Error while tearing down prometheus stack: %v", err)
		}
	}
	if suiteSummary.NumberOfFailedSpecs > 0 {
		klog.Exitf("%d tests have failed!", suiteSummary.NumberOfFailedSpecs)
	}
}
