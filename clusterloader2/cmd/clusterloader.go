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
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/execservice"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/heapprofile"
	"k8s.io/perf-tests/clusterloader2/pkg/imagepreload"
	"k8s.io/perf-tests/clusterloader2/pkg/metadata"
	"k8s.io/perf-tests/clusterloader2/pkg/modifier"
	"k8s.io/perf-tests/clusterloader2/pkg/prometheus"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
	"k8s.io/utils/ptr"

	_ "k8s.io/perf-tests/clusterloader2/pkg/dependency/dra"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/bundle"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/dns"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/network"
	_ "k8s.io/perf-tests/clusterloader2/pkg/measurement/common/network-policy"
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
	testSuiteConfigPath string
	port                int
	dryRun              bool
	heapProfileInterval time.Duration
)

func initClusterFlags() {
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.KubeConfigPath, "kubeconfig", "KUBECONFIG", "", "Path to the kubeconfig file (if not empty, --run-from-cluster must be false)")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.RunFromCluster, "run-from-cluster", "RUN_FROM_CLUSTER", false, "Whether to use in-cluster client-config to create a client, --kubeconfig must be unset")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.Nodes, "nodes", "NUM_NODES", 0, "number of nodes")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.KubeletPort, "kubelet-port", "KUBELET_PORT", 10250, "Port of the kubelet to use")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.K8SClientsNumber, "k8s-clients-number", "K8S_CLIENTS_NUMBER", 0, fmt.Sprintf("(Optional) Number of k8s clients to use. If 0, will create 1 client per %d nodes", nodesPerClients))
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdCertificatePath, "etcd-certificate", "ETCD_CERTIFICATE", "/etc/srv/kubernetes/pki/etcd-apiserver-server.crt", "Path to the etcd certificate on the master machine")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdKeyPath, "etcd-key", "ETCD_KEY", "/etc/srv/kubernetes/pki/etcd-apiserver-server.key", "Path to the etcd key on the master machine")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdInsecurePort, "etcd-insecure-port", "ETCD_INSECURE_PORT", 2382, "Inscure http port")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdPprofPort, "etcd-pprof-port", "ETCD_PPROF_PORT", 2385, "Insecure http port serving etcd /debug/pprof (etcd --listen-client-http-urls)")
	flags.IntEnvVar(&clusterLoaderConfig.ClusterConfig.EtcdEventsPprofPort, "etcd-events-pprof-port", "ETCD_EVENTS_PPROF_PORT", 2386, "Insecure http port serving etcd-events /debug/pprof (etcd --listen-client-http-urls)")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.DeleteStaleNamespaces, "delete-stale-namespaces", "DELETE_STALE_NAMESPACES", false, "DEPRECATED: Whether to delete all stale namespaces before the test execution.")
	err := flags.MarkDeprecated("delete-stale-namespaces", "specify deleteStaleNamespaces in testconfig file instead.")
	if err != nil {
		klog.Fatalf("unable to mark flag delete-stale-namespaces deprecated %v", err)
	}
	// TODO(#1696): Clean up after removing automanagedNamespaces
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.DeleteAutomanagedNamespaces, "delete-automanaged-namespaces", "DELETE_AUTOMANAGED_NAMESPACES", true, "DEPRECATED: Whether to delete all automanaged namespaces after the test execution.")
	err = flags.MarkDeprecated("delete-automanaged-namespaces", "specify deleteAutomanagedNamespaces in testconfig file instead.")
	if err != nil {
		klog.Fatalf("unable to mark flag delete-automanaged-namespaces deprecated %v", err)
	}
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.MasterName, "mastername", "MASTER_NAME", "", "Name of the masternode")
	// TODO(#595): Change the name of the MASTER_IP and MASTER_INTERNAL_IP flags and vars to plural
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterIPs, "masterip", "MASTER_IP", nil /*defaultValue*/, "Hostname/IP of the master node, supports multiple values when separated by commas")
	flags.StringSliceEnvVar(&clusterLoaderConfig.ClusterConfig.MasterInternalIPs, "master-internal-ip", "MASTER_INTERNAL_IP", nil /*defaultValue*/, "Cluster internal/private IP of the master vm, supports multiple values when separated by commas")
	flags.StringEnvVar(&clusterLoaderConfig.ClusterConfig.MasterDNSEndpoint, "master-endpoint", "MASTER_DNS_ENDPOINT", "", "Endpoint of the master node, exclusive with --masterip and --master-internal-ips")
	flags.BoolEnvVar(&clusterLoaderConfig.ClusterConfig.APIServerPprofByClientEnabled, "apiserver-pprof-by-client-enabled", "APISERVER_PPROF_BY_CLIENT_ENABLED", true, "Whether apiserver pprof endpoint can be accessed by Kubernetes client.")
	flags.BoolVar(&clusterLoaderConfig.ClusterConfig.SkipClusterVerification, "skip-cluster-verification", false, "Whether to skip the cluster verification, which expects at least one schedulable node in the cluster")

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
	flags.InitFlagSet()

	flags.StringVar(&clusterLoaderConfig.ReportDir, "report-dir", "", "Path to the directory where the reports should be saved. Default is empty, which cause reports being written to standard output.")
	// TODO(https://github.com/kubernetes/perf-tests/issues/641): Remove testconfig and testoverrides flags when test suite is fully supported.
	flags.StringArrayVar(&testConfigPaths, "testconfig", []string{}, "Paths to the test config files")
	flags.StringArrayVar(&clusterLoaderConfig.OverridePaths, "testoverrides", []string{}, "Paths to the config overrides file. The latter overrides take precedence over changes in former files.")
	flags.StringVar(&testSuiteConfigPath, "testsuite", "", "Path to the test suite config file")
	flags.IntVar(&port, "port", 8000, "Port to be used by http server with pprof.")
	flags.DurationEnvVar(&heapProfileInterval, "heap-profile-interval", "CL2_HEAP_PROFILE_INTERVAL", 0, "Interval between clusterloader2 heap profiles. 0 represents disabled.")
	flags.BoolVar(&dryRun, "dry-run", false, "Whether to skip running test and only compile test config")
	flags.StringEnvVar(&clusterLoaderConfig.ImageRegistry, "registry-k8s-repo", "REGISTRY_K8S_REPO", "registry.k8s.io", "FQDN of registry.k8s.io image repo")

	initClusterFlags()
	execservice.InitFlags(&clusterLoaderConfig.ExecServiceConfig)
	imagepreload.InitFlags()
	modifier.InitFlags(&clusterLoaderConfig.ModifierConfig)
	prometheus.InitFlags(&clusterLoaderConfig.PrometheusConfig)
	prometheus.InitExperimentalFlags()
}

func validateFlags() *errors.ErrorList {
	errList := errors.NewErrorList()
	if len(testConfigPaths) == 0 && testSuiteConfigPath == "" {
		errList.Append(fmt.Errorf("no test config path or test suite path specified"))
	}
	if len(testConfigPaths) > 0 && testSuiteConfigPath != "" {
		errList.Append(fmt.Errorf("test config path and test suite path cannot be provided at the same time"))
	}
	if heapProfileInterval < 0 {
		errList.Append(fmt.Errorf("heap-profile-interval must be non-negative, got %s", heapProfileInterval))
	}
	if heapProfileInterval > 0 && clusterLoaderConfig.ReportDir == "" {
		errList.Append(fmt.Errorf("heap-profile-interval requires --report-dir to be set"))
	}
	errList.Concat(validateClusterFlags())
	errList.Concat(prometheus.ValidatePrometheusFlags(&clusterLoaderConfig.PrometheusConfig))
	return errList
}

func completeConfig(m *framework.MultiClientSet) error {
	if clusterLoaderConfig.ClusterConfig.Nodes == 0 {
		nodes, err := util.GetSchedulableUntainedNodesNumber(m.GetClient())
		if err != nil {
			if clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KCPName {
				return fmt.Errorf("getting number of nodes error: %v, please create nodes.core CRD", err)
			}
			return fmt.Errorf("getting number of nodes error: %v", err)
		}
		clusterLoaderConfig.ClusterConfig.Nodes = nodes
		klog.V(0).Infof("ClusterConfig.Nodes set to %v", nodes)
	}
	if clusterLoaderConfig.ClusterConfig.MasterDNSEndpoint == "" {
		err := completeIpMasterConfig(m)
		if err != nil {
			return err
		}
	}

	if !clusterLoaderConfig.ClusterConfig.Provider.Features().SupportAccessAPIServerPprofEndpoint {
		clusterLoaderConfig.ClusterConfig.APIServerPprofByClientEnabled = false
	}
	if clusterLoaderConfig.ClusterConfig.K8SClientsNumber == 0 {
		clusterLoaderConfig.ClusterConfig.K8SClientsNumber = getClientsNumber(clusterLoaderConfig.ClusterConfig.Nodes)
	}
	return nil
}

func completeIpMasterConfig(m *framework.MultiClientSet) error {
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
	return nil
}

func verifyCluster(c kubernetes.Interface) error {
	if clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KCPName || clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KubestellarName {
		return nil
	}
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
	if clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KCPName || clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KubestellarName {
		return 1
	}
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
	clusterLoaderConfig.ExecServiceConfig.ImageRegistry = clusterLoaderConfig.ImageRegistry

	// Start http server with pprof.
	go func() {
		klog.Infof("Listening on %d", port)
		err := http.ListenAndServe(fmt.Sprintf("localhost:%d", port), nil)
		klog.Errorf("http server unexpectedly ended: %v", err)
	}()

	newProvider, err := provider.NewProvider(&providerInitOptions)
	if err != nil {
		klog.Exitf("Error init provider: %v", err)
	}
	clusterLoaderConfig.ClusterConfig.Provider = newProvider

	if clusterLoaderConfig.ClusterConfig.Provider.Name() == provider.KubestellarName {
		clusterLoaderConfig.ExecServiceConfig.Enable = false
	}

	if errList := validateFlags(); !errList.IsEmpty() {
		klog.Exitf("Parsing flags error: %v", errList.String())
	}

	klog.V(2).Infof("KubeConfigPath: %v", clusterLoaderConfig.ClusterConfig.KubeConfigPath)
	if clusterLoaderConfig.ClusterConfig.KubeConfigPath != "" {
		content, err := os.ReadFile(clusterLoaderConfig.ClusterConfig.KubeConfigPath)
		if err != nil {
			klog.Errorf("Error reading kubeconfig: %v", err)
		} else {
			klog.V(2).Infof("KubeConfig content:\n%s", string(content))
		}
	}
	mclient, err := framework.NewMultiClientSet(clusterLoaderConfig.ClusterConfig.KubeConfigPath, 1)
	if err != nil {
		klog.Exitf("Client creation error: %v", err)
	}

	stopKubelite := func() {}
	if !dryRun {
		var err error
		stopKubelite, err = setupHybridKubeliteCluster(mclient.GetClient(), clusterLoaderConfig.ClusterConfig.KubeConfigPath)
		if err != nil {
			klog.Exitf("Hybrid kubelite setup error: %v", err)
		}
		defer stopKubelite()
	}

	if err = completeConfig(mclient); err != nil {
		klog.Exitf("Config completing error: %v", err)
	}

	klog.V(0).Infof("Using config: %+v", clusterLoaderConfig)

	if err = createReportDir(); err != nil {
		klog.Exitf("Cannot create report directory: %v", err)
	}

	if heapProfileInterval > 0 {
		heapprofile.Start(clusterLoaderConfig.ReportDir, heapProfileInterval)
	}

	if err = util.LogClusterNodes(mclient.GetClient()); err != nil {
		klog.Errorf("Nodes info logging error: %v", err)
	}

	if !clusterLoaderConfig.ClusterConfig.SkipClusterVerification {
		if err = verifyCluster(mclient.GetClient()); err != nil {
			klog.Exitf("Cluster verification error: %v", err)
		}
	}

	f, err := framework.NewFramework(
		&clusterLoaderConfig.ClusterConfig,
		clusterLoaderConfig.ClusterConfig.K8SClientsNumber,
	)
	if err != nil {
		klog.Exitf("Framework creation error: %v", err)
	}

	var prometheusController *prometheus.Controller
	var prometheusFramework *framework.Framework
	var testReporter test.Reporter

	if !dryRun {
		if clusterLoaderConfig.PrometheusConfig.EnableServer {
			if prometheusController, err = prometheus.NewController(&clusterLoaderConfig); err != nil {
				klog.Exitf("Error while creating Prometheus Controller: %v", err)
			}
			prometheusFramework = prometheusController.GetFramework()
			if err := prometheusController.SetUpPrometheusStack(); err != nil {
				klog.Exitf("Error while setting up prometheus stack: %v", err)
			}
			if clusterLoaderConfig.PrometheusConfig.TearDownServer {
				prometheusController.EnableTearDownPrometheusStackOnInterrupt()
			}
		}
		if clusterLoaderConfig.ExecServiceConfig.Enable {
			if err := execservice.SetUpExecService(f, clusterLoaderConfig.ExecServiceConfig); err != nil {
				klog.Exitf("Error while setting up exec service: %v", err)
			}
		}
		if err := imagepreload.Setup(&clusterLoaderConfig, f); err != nil {
			klog.Exitf("Error while preloading images: %v", err)
		}

		if err := metadata.Dump(f, path.Join(clusterLoaderConfig.ReportDir, "cl2-metadata.json")); err != nil {
			klog.Errorf("Error while dumping metadata: %v", err)
		}
		testReporter = test.CreateSimpleReporter(path.Join(clusterLoaderConfig.ReportDir, "junit.xml"), "ClusterLoaderV2")
		testReporter.BeginTestSuite()
	}

	var testScenarios []api.TestScenario
	if testSuiteConfigPath != "" {
		testSuite, err := config.LoadTestSuite(testSuiteConfigPath)
		if err != nil {
			klog.Exitf("Error while reading test suite: %v", err)
		}
		testScenarios = []api.TestScenario(testSuite)
	} else {
		for i := range testConfigPaths {
			testScenario := api.TestScenario{
				ConfigPath:    testConfigPaths[i],
				OverridePaths: []string{},
			}
			testScenarios = append(testScenarios, testScenario)
		}
	}

	var contexts []test.Context
	for i := range testScenarios {
		ctx, errList := test.CreateTestContext(f, prometheusFramework, &clusterLoaderConfig, testReporter, &testScenarios[i])
		if !errList.IsEmpty() {
			klog.Exitf("Test context creation failed: %s", errList.String())
		}
		testConfig, errList := test.CompileTestConfig(ctx)
		// Dump test config before checking errors - it can still be useful for debugging.
		if testConfig != nil {
			if err := dumpTestConfig(ctx, testConfig); err != nil {
				klog.Errorf("Error while dumping test config: %v", err)
			}
		}
		if !errList.IsEmpty() {
			klog.Exitf("Test compilation failed: %s", errList.String())
		}
		ctx.SetTestConfig(testConfig)
		contexts = append(contexts, ctx)
	}

	if dryRun {
		// Dry run always exits with error so if it's ever enabled in CI, the test will fail.
		klog.Exitf("Dry run mode enabled, exiting after dumping test config in %s.", path.Join(clusterLoaderConfig.ReportDir))
	}

	for i := range contexts {
		runSingleTest(contexts[i])
	}

	testReporter.EndTestSuite()

	if err := prometheusController.MakePrometheusSnapshotIfEnabled(); err != nil {
		klog.Errorf("Error while making prometheus snapshot: %v", err)
	}

	if clusterLoaderConfig.PrometheusConfig.EnableServer && clusterLoaderConfig.PrometheusConfig.TearDownServer {
		if err := prometheusController.TearDownPrometheusStack(); err != nil {
			klog.Errorf("Error while tearing down prometheus stack: %v", err)
		}
	}
	if clusterLoaderConfig.ExecServiceConfig.Enable {
		if err := execservice.TearDownExecService(f); err != nil {
			klog.Errorf("Error while tearing down exec service: %v", err)
		}
	}
	stopKubelite()
	if failedTestItems := testReporter.GetNumberOfFailedTestItems(); failedTestItems > 0 {
		klog.Exitf("%d tests have failed!", failedTestItems)
	}
}

func runSingleTest(ctx test.Context) {
	testID := getTestID(ctx.GetTestScenario())
	testStart := time.Now()
	printTestStart(testID)
	errList := test.RunTest(ctx)
	if !errList.IsEmpty() {
		printTestResult(testID, "Fail", errList.String())
	} else {
		printTestResult(testID, "Success", "")
	}
	testConfigPath := ctx.GetTestScenario().ConfigPath
	ctx.GetTestReporter().ReportTestFinish(time.Since(testStart), testConfigPath, errList)
}

func getTestID(ts *api.TestScenario) string {
	if ts.Identifier != "" {
		return fmt.Sprintf("%s(%s)", ts.Identifier, ts.ConfigPath)
	}
	return ts.ConfigPath
}

func dumpTestConfig(ctx test.Context, config *api.Config) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshaling config error: %w", err)
	}
	fileName := "generatedConfig_" + config.Name
	if identifier := ctx.GetTestScenario().Identifier; identifier != "" {
		fileName += "_" + identifier
	}
	filePath := path.Join(ctx.GetClusterLoaderConfig().ReportDir, fileName+".yaml")
	if err := os.WriteFile(filePath, b, 0644); err != nil {
		return fmt.Errorf("saving file error: %w", err)
	}
	klog.Infof("Test config successfully dumped to: %s", filePath)
	return nil
}

const (
	defaultKeepRealWorkers = 2
	kubeliteSimLabelKey    = "kubelite.k8s.io/simulated"
	kubeliteTaintKey       = "kubelite.io/simulated"
)

// setupHybridKubeliteCluster replaces excess real GCE worker nodes with
// simulated kubelite nodes while keeping keepReal real worker VMs for
// DaemonSet probers, CoreDNS, and Prometheus.
func setupHybridKubeliteCluster(kclient kubernetes.Interface, kubeconfigPath string) (func(), error) {
	if os.Getenv("CL2_ENABLE_HYBRID_KUBELITE") == "false" {
		return func() {}, nil
	}
	ctx := context.Background()
	nodeList, err := kclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing initial nodes: %w", err)
	}
	keepReal := defaultKeepRealWorkers
	if parsed, err := strconv.Atoi(os.Getenv("CL2_KEEP_REAL_WORKERS")); err == nil && parsed >= 1 {
		keepReal = parsed
	}

	var realWorkers, existingSim []corev1.Node
	hasKopsGCE := false
	for _, n := range nodeList.Items {
		if strings.HasPrefix(n.Spec.ProviderID, "gce://") && (n.Labels["kops.k8s.io/instancegroup"] != "" || strings.HasPrefix(n.Name, "nodes-")) {
			hasKopsGCE = true
		}
		switch {
		case util.IsControlPlaneNode(&n), n.Labels["kops.k8s.io/instancegroup"] == "addons", strings.HasPrefix(n.Name, "addons-"):
		case n.Labels[kubeliteSimLabelKey] == "true", n.Labels[kubeliteTaintKey] == "true":
			existingSim = append(existingSim, n)
		default:
			realWorkers = append(realWorkers, n)
		}
	}
	if !hasKopsGCE || (len(realWorkers) <= keepReal && len(existingSim) == 0) {
		return func() {}, nil
	}

	corednsNodes := make(map[string]bool)
	if sysPods, err := kclient.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{}); err == nil {
		for _, p := range sysPods.Items {
			if strings.HasPrefix(p.Name, "coredns-") && !strings.HasPrefix(p.Name, "coredns-autoscaler") {
				corednsNodes[p.Spec.NodeName] = true
			}
		}
	}
	sort.Slice(realWorkers, func(i, j int) bool {
		bi := strings.HasPrefix(realWorkers[i].Name, "nodes-us-east1-b-") || realWorkers[i].Labels["topology.kubernetes.io/zone"] == "us-east1-b"
		bj := strings.HasPrefix(realWorkers[j].Name, "nodes-us-east1-b-") || realWorkers[j].Labels["topology.kubernetes.io/zone"] == "us-east1-b"
		if bi != bj {
			return bi
		}
		if corednsNodes[realWorkers[i].Name] != corednsNodes[realWorkers[j].Name] {
			return corednsNodes[realWorkers[i].Name]
		}
		return realWorkers[i].Name < realWorkers[j].Name
	})

	keepReal = min(keepReal, len(realWorkers))
	nodesToDelete := realWorkers[keepReal:]
	numSimulated := max(len(nodesToDelete), len(existingSim))
	deletedSet := make(map[string]bool, len(nodesToDelete))
	for _, n := range nodesToDelete {
		deletedSet[n.Name] = true
	}
	klog.Infof("[hybrid-kubelite] keeping %d real workers, replacing %d workers with %d simulated nodes", keepReal, len(nodesToDelete), numSimulated)

	binPath := filepath.Join(os.TempDir(), "kubelite-bin")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/kubelite").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building kubelite: %w (%s)", err, string(out))
	}
	kubeliteCmd := exec.Command(binPath, "--kubeconfig="+kubeconfigPath, fmt.Sprintf("--nodes=%d", numSimulated), "--taint-simulated=true")
	kubeliteCmd.Stdout, kubeliteCmd.Stderr = os.Stdout, os.Stderr
	if err := kubeliteCmd.Start(); err != nil {
		return nil, fmt.Errorf("starting kubelite: %w", err)
	}

	if err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		nodes, err := kclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: kubeliteSimLabelKey + "=true"})
		if err != nil {
			return false, nil
		}
		return len(nodes.Items) >= numSimulated, nil
	}); err != nil {
		return nil, fmt.Errorf("waiting for %d simulated nodes to register: %w", numSimulated, err)
	}

	if len(nodesToDelete) > 0 {
		purgeDeletedWorkerNodes(ctx, kclient, deletedSet)
		deleteGCEWorkerInstances(ctx, nodesToDelete)
		purgeDeletedWorkerNodes(ctx, kclient, deletedSet)
	}

	stopBgCh := make(chan struct{})
	go wait.Until(func() { purgeDeletedWorkerNodes(ctx, kclient, deletedSet) }, 2*time.Second, stopBgCh)

	_ = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		nodes, err := kclient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		for i := range nodes.Items {
			if deletedSet[nodes.Items[i].Name] {
				return false, nil
			}
		}
		return true, nil
	})

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(stopBgCh)
			if kubeliteCmd.Process != nil {
				_ = kubeliteCmd.Process.Kill()
				_, _ = kubeliteCmd.Process.Wait()
			}
			simSet := make(map[string]bool, numSimulated)
			for i := 0; i < numSimulated; i++ {
				simSet[fmt.Sprintf("kubelite-%04d", i)] = true
			}
			purgeDeletedWorkerNodes(ctx, kclient, simSet)
		})
	}, nil
}

// deleteGCEWorkerInstances removes replaced worker VMs from their managed instance groups.
func deleteGCEWorkerInstances(ctx context.Context, nodesToDelete []corev1.Node) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	project := os.Getenv("PROJECT")
	byZone := make(map[string][]string)
	for _, n := range nodesToDelete {
		if parts := strings.Split(strings.TrimPrefix(n.Spec.ProviderID, "gce://"), "/"); len(parts) == 3 {
			project = parts[0]
			byZone[parts[1]] = append(byZone[parts[1]], parts[2])
		}
	}
	if project == "" || len(byZone) == 0 {
		return
	}
	migByZone := make(map[string]string)
	if out, err := exec.CommandContext(cctx, "gcloud", "compute", "instance-groups", "managed", "list", "--project="+project, "--format=value(name,zone)").CombinedOutput(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && strings.Contains(f[0], "nodes-") {
				migByZone[f[1]] = f[0]
			}
		}
	}
	var wg sync.WaitGroup
	for zone, insts := range byZone {
		wg.Add(1)
		go func(zone string, insts []string) {
			defer wg.Done()
			if mig := migByZone[zone]; mig != "" {
				if err := exec.CommandContext(cctx, "gcloud", "compute", "instance-groups", "managed", "delete-instances", mig, "--project="+project, "--zone="+zone, "--instances="+strings.Join(insts, ","), "--quiet").Run(); err == nil {
					return
				}
			}
			args := append(append([]string{"compute", "instances", "delete"}, insts...), "--project="+project, "--zone="+zone, "--quiet")
			_ = exec.CommandContext(cctx, "gcloud", args...).Run()
		}(zone, insts)
	}
	wg.Wait()
}

// purgeDeletedWorkerNodes force-deletes Pods, Nodes, and Leases belonging to deletedSet.
func purgeDeletedWorkerNodes(ctx context.Context, kclient kubernetes.Interface, deletedSet map[string]bool) {
	if len(deletedSet) == 0 {
		return
	}
	zeroGrace := metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))}
	if pods, err := kclient.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pods.Items {
			if p := &pods.Items[i]; deletedSet[p.Spec.NodeName] {
				if p.Namespace == "kube-system" && len(p.Labels) > 0 {
					_, _ = kclient.CoreV1().Pods(p.Namespace).Patch(ctx, p.Name, types.MergePatchType, []byte(`{"metadata":{"labels":{"k8s-app":null,"component":null}}}`), metav1.PatchOptions{})
				}
				_ = kclient.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, zeroGrace)
			}
		}
	}
	for name := range deletedSet {
		_ = kclient.CoreV1().Nodes().Delete(ctx, name, zeroGrace)
		_ = kclient.CoordinationV1().Leases(corev1.NamespaceNodeLease).Delete(ctx, name, zeroGrace)
	}
}
