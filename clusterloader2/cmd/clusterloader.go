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
	"k8s.io/kubernetes/pkg/master/ports"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/execservice"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/imagepreload"
	"k8s.io/perf-tests/clusterloader2/pkg/modifier"
	"k8s.io/perf-tests/clusterloader2/pkg/prometheus"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
	"k8s.io/perf-tests/clusterloader2/pkg/util"

	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/bundle"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/probes"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/slos"
)

const (
	dashLine        = "--------------------------------------------------------------------------------"
	nodesPerClients = 100
)

var (
	clusterLoaderConfig config.ClusterLoaderConfig
	providerInitOptions provider.InitOptions
	testConfigPaths     []string
	testOverridePaths   []string
	testSuiteConfigPath string
)

func initClusterFlags() {
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.KubeConfigPath, "kubeconfig", "KUBECONFIG", "", "Path to the kubeconfig file (if not empty, --run-from-cluster must be false)")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.RunFromCluster, "run-from-cluster", "RUN_FROM_CLUSTER", false, "Whether to use in-cluster client-config to create a client, --kubeconfig must be unset")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.Nodes, "nodes", "NUM_NODES", 0, "number of nodes")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.KubeletPort, "kubelet-port", "KUBELET_PORT", ports.KubeletPort, "Port of the kubelet to use")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdCertificatePath, "etcd-certificate", "ETCD_CERTIFICATE", "/etc/srv/kubernetes/pki/etcd-apiserver-server.crt", "Path to the etcd certificate on the master machine")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdKeyPath, "etcd-key", "ETCD_KEY", "/etc/srv/kubernetes/pki/etcd-apiserver-server.key", "Path to the etcd key on the master machine")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdInsecurePort, "etcd-insecure-port", "ETCD_INSECURE_PORT", 2382, "Inscure http port")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.DeleteStaleNamespaces, "delete-stale-namespaces", "DELETE_STALE_NAMESPACES", false, "DEPRECATED: Whether to delete all stale namespaces before the test execution.")
	flags.MarkDeprecated("delete-stale-namespaces", "specify deleteStaleNamespaces in testconfig file instead.")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.DeleteAutomanagedNamespaces, "delete-automanaged-namespaces", "DELETE_AUTOMANAGED_NAMESPACES", true, "DEPRECATED: Whether to delete all automanaged namespaces after the test execution.")
	flags.MarkDeprecated("delete-automanaged-namespaces", "specify deleteAutomanagedNamespaces in testconfig file instead.")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.MasterName, "mastername", "MASTER_NAME", "", "Name of the masternode")
	// TODO(#595): Change the name of the MASTER_IP and MASTER_INTERNAL_IP flags and vars to plural
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterIPs, "masterip", "MASTER_IP", nil /*defaultValue*/, "Hostname/IP of the master node, supports multiple values when separated by commas")
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterInternalIPs, "master-internal-ip", "MASTER_INTERNAL_IP", nil /*defaultValue*/, "Cluster internal/private IP of the master vm, supports multiple values when separated by commas")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.APIServerPprofByClientEnabled, "apiserver-pprof-by-client-enabled", "APISERVER_PPROF_BY_CLIENT_ENABLED", true, "Whether apiserver pprof endpoint can be accessed by Kubernetes client.")

	flags.StringEnvVar(&providerInitOptions.ProviderName, "provider", "PROVIDER", "", "Cluster provider name")
	flags.StringSliceEnvVar(&providerInitOptions.ProviderConfigs, "provider-configs", "PROVIDER_CONFIGS", nil, "Cluster provider configurations")
	flags.StringEnvVar(&providerInitOptions.KubemarkRootKubeConfigPath, "kubemark-root-kubeconfig", "KUBEMARK_ROOT_KUBECONFIG", "",
		"DEPRECATED: Please use provider-config=\"ROOT_KUBECONFIG=<value>\". Path the to kubemark root kubeconfig file, i.e. kubeconfig of the cluster where kubemark cluster is run. Ignored if provider != kubemark")
}

func validateClusterFlags() *errors.ErrorList {
	errList := errors.NewErrorList()

	// if '--run-from-cluster=true', create in-cluster config and validate kubeconfig is unset
	// if '--run-from-cluster=false', use kubeconfig (and validate it is set)
	switch clusterLoaderConfig.ClusterConfig.RunFromCluster {
	case true:
		if clusterLoaderConfig.ClusterConfig.KubeConfigPath != "" {
			errList.Append(fmt.Errorf("unexpected kubeconfig path specified %q when --run-from-cluster is set", clusterLoaderConfig.ClusterConfig.KubeConfigPath))
		}
	case false:
		if clusterLoaderConfig.ClusterConfig.KubeConfigPath == "" {
			errList.Append(fmt.Errorf("no kubeconfig path specified when --run-from-cluster is unset"))
		}
	}
	if clusterLoaderConfig.PrometheusConfig.EnableServer {
		if !clusterLoaderConfig.ClusterConfig.Provider.Features().SupportEnablePrometheusServer {
			errList.Append(fmt.Errorf("cannot enable prometheus server for provider %s", clusterLoaderConfig.ClusterConfig.Provider.Name()))
		}
	}
	return errList
}

func initFlags() {
	flags.StringVar(&clusterLoaderConfig.ReportDir, "report-dir", "", "Path to the directory where the reports should be saved. Default is empty, which cause reports being written to standard output.")
	flags.BoolEnvVar(&clusterLoaderConfig.EnableExecService, "enable-exec-service", "ENABLE_EXEC_SERVICE", false, "Whether to enable exec service that allows executing arbitrary commands from a pod running in the cluster.")
	// TODO(https://github.com/kubernetes/perf-tests/issues/641): Remove testconfig and testoverrides flags when test suite is fully supported.
	flags.StringArrayVar(&testConfigPaths, "testconfig", []string{}, "Paths to the test config files")
	flags.StringArrayVar(&testOverridePaths, "testoverrides", []string{}, "Paths to the config overrides file. The latter overrides take precedence over changes in former files.")
	flags.StringVar(&testSuiteConfigPath, "testsuite", "", "Path to the test suite config file")
	initClusterFlags()
	modifier.InitFlags(&clusterLoaderConfig.ModifierConfig)
	prometheus.InitFlags(&clusterLoaderConfig.PrometheusConfig)
}

func validateFlags() *errors.ErrorList {
	errList := errors.NewErrorList()
	if len(testConfigPaths) == 0 && testSuiteConfigPath == "" {
		errList.Append(fmt.Errorf("no test config path or test suite path specified"))
	}
	if len(testConfigPaths) > 0 && testSuiteConfigPath != "" {
		errList.Append(fmt.Errorf("test config path and test suite path cannot be provided at the same time"))
	}
	errList.Concat(validateClusterFlags())
	errList.Concat(prometheus.ValidatePrometheusFlags(&clusterLoaderConfig.PrometheusConfig))
	return errList
}

func completeConfig(m *framework.MultiClientSet) error {
	if clusterLoaderConfig.ClusterConfig.Nodes == 0 {
		nodes, err := util.GetSchedulableUntainedNodesNumber(m.GetClient())
		if err != nil {
			return fmt.Errorf("getting number of nodes error: %v", err)
		}
		clusterLoaderConfig.ClusterConfig.Nodes = nodes
		klog.V(0).Infof("ClusterConfig.Nodes set to %v", nodes)
	}
	if clusterLoaderConfig.ClusterConfig.MasterName == "" {
		masterName, err := util.GetMasterName(m.GetClient())
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterName = masterName
			klog.V(0).Infof("ClusterConfig.MasterName set to %v", masterName)
		} else {
			klog.Errorf("Getting master name error: %v", err)
		}
	}
	if len(clusterLoaderConfig.ClusterConfig.MasterIPs) == 0 {
		masterIPs, err := util.GetMasterIPs(m.GetClient(), corev1.NodeExternalIP)
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterIPs = masterIPs
			klog.V(0).Infof("ClusterConfig.MasterIP set to %v", masterIPs)
		} else {
			klog.Errorf("Getting master external ip error: %v", err)
		}
	}
	if len(clusterLoaderConfig.ClusterConfig.MasterInternalIPs) == 0 {
		masterIPs, err := util.GetMasterIPs(m.GetClient(), corev1.NodeInternalIP)
		if err == nil {
			clusterLoaderConfig.ClusterConfig.MasterInternalIPs = masterIPs
			klog.V(0).Infof("ClusterConfig.MasterInternalIP set to %v", masterIPs)
		} else {
			klog.Errorf("Getting master internal ip error: %v", err)
		}
	}

	if !clusterLoaderConfig.ClusterConfig.Provider.Features().SupportAccessAPIServerPprofEndpoint {
		clusterLoaderConfig.ClusterConfig.APIServerPprofByClientEnabled = false
	}
	prometheus.CompleteConfig(&clusterLoaderConfig.PrometheusConfig)
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
	klog.V(0).Infof(dashLine)
	klog.V(0).Infof("Running %v", name)
	klog.V(0).Infof(dashLine)
}

func printTestResult(name, status, errors string) {
	logf := klog.V(0).Infof
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

	provider, err := provider.NewProvider(&providerInitOptions)
	if err != nil {
		klog.Exitf("Error init provider: %v", err)
	}
	clusterLoaderConfig.ClusterConfig.Provider = provider

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

	klog.V(0).Infof("Using config: %+v", clusterLoaderConfig)

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

	var prometheusController *prometheus.Controller
	var prometheusFramework *framework.Framework
	if clusterLoaderConfig.PrometheusConfig.EnableServer {
		// Pass overrides to prometheus controller
		clusterLoaderConfig.TestScenario.OverridePaths = testOverridePaths
		if prometheusController, err = prometheus.NewController(&clusterLoaderConfig); err != nil {
			klog.Exitf("Error while creating Prometheus Controller: %v", err)
		}
		prometheusFramework = prometheusController.GetFramework()
		if err := prometheusController.SetUpPrometheusStack(); err != nil {
			klog.Exitf("Error while setting up prometheus stack: %v", err)
		}
	}
	if clusterLoaderConfig.EnableExecService {
		if err := execservice.SetUpExecService(f); err != nil {
			klog.Exitf("Error while setting up exec service: %v", err)
		}
	}
	if err := imagepreload.Setup(&clusterLoaderConfig, f); err != nil {
		klog.Exitf("Error while preloading images: %v", err)
	}

	suiteSummary := &ginkgotypes.SuiteSummary{
		SuiteDescription:           "ClusterLoaderV2",
		NumberOfSpecsThatWillBeRun: len(testConfigPaths),
	}
	junitReporter := ginkgoreporters.NewJUnitReporter(path.Join(clusterLoaderConfig.ReportDir, "junit.xml"))
	junitReporter.SpecSuiteWillBegin(ginkgoconfig.GinkgoConfig, suiteSummary)
	testsStart := time.Now()
	if testSuiteConfigPath != "" {
		testSuite, err := config.LoadTestSuite(testSuiteConfigPath)
		if err != nil {
			klog.Exitf("Error while reading test suite: %v", err)
		}
		for i := range testSuite {
			clusterLoaderConfig.TestScenario = testSuite[i]
			runSingleTest(f, prometheusFramework, junitReporter, suiteSummary)
		}
	} else {
		for i := range testConfigPaths {
			clusterLoaderConfig.TestScenario.ConfigPath = testConfigPaths[i]
			clusterLoaderConfig.TestScenario.OverridePaths = testOverridePaths
			runSingleTest(f, prometheusFramework, junitReporter, suiteSummary)
		}
	}
	suiteSummary.RunTime = time.Since(testsStart)
	junitReporter.SpecSuiteDidEnd(suiteSummary)

	if clusterLoaderConfig.PrometheusConfig.EnableServer && clusterLoaderConfig.PrometheusConfig.TearDownServer {
		if err := prometheusController.TearDownPrometheusStack(); err != nil {
			klog.Errorf("Error while tearing down prometheus stack: %v", err)
		}
	}
	if clusterLoaderConfig.EnableExecService {
		if err := execservice.TearDownExecService(f); err != nil {
			klog.Errorf("Error while tearing down exec service: %v", err)
		}
	}
	if suiteSummary.NumberOfFailedSpecs > 0 {
		klog.Exitf("%d tests have failed!", suiteSummary.NumberOfFailedSpecs)
	}
}

func runSingleTest(
	f *framework.Framework,
	prometheusFramework *framework.Framework,
	junitReporter *ginkgoreporters.JUnitReporter,
	suiteSummary *ginkgotypes.SuiteSummary,
) {
	testID := getTestID(clusterLoaderConfig.TestScenario)
	testStart := time.Now()
	specSummary := &ginkgotypes.SpecSummary{
		ComponentTexts: []string{suiteSummary.SuiteDescription, testID},
	}
	printTestStart(testID)
	if errList := test.RunTest(f, prometheusFramework, &clusterLoaderConfig); !errList.IsEmpty() {
		suiteSummary.NumberOfFailedSpecs++
		specSummary.State = ginkgotypes.SpecStateFailed
		specSummary.Failure = ginkgotypes.SpecFailure{
			Message: errList.String(),
		}
		printTestResult(testID, "Fail", errList.String())
	} else {
		specSummary.State = ginkgotypes.SpecStatePassed
		printTestResult(testID, "Success", "")
	}
	specSummary.RunTime = time.Since(testStart)
	junitReporter.SpecDidComplete(specSummary)
}

func getTestID(ts api.TestScenario) string {
	if ts.Identifier != "" {
		return fmt.Sprintf("%s(%s)", ts.Identifier, ts.ConfigPath)
	}
	return ts.ConfigPath
}
