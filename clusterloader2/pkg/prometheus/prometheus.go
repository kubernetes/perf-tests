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

package prometheus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/system"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	namespace                    = "monitoring"
	coreManifests                = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/*.yaml"
	defaultServiceMonitors       = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/default/*.yaml"
	masterIPServiceMonitors      = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/master-ip/*.yaml"
	checkPrometheusReadyInterval = 30 * time.Second
	checkPrometheusReadyTimeout  = 15 * time.Minute
	numK8sClients                = 1
	nodeExporterPod              = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/exporters/node-exporter.yaml"
)

// InitFlags initializes prometheus flags.
func InitFlags(p *config.PrometheusConfig) {
	flags.BoolEnvVar(&p.EnableServer, "enable-prometheus-server", "ENABLE_PROMETHEUS_SERVER", false, "Whether to set-up the prometheus server in the cluster.")
	flags.BoolEnvVar(&p.TearDownServer, "tear-down-prometheus-server", "TEAR_DOWN_PROMETHEUS_SERVER", true, "Whether to tear-down the prometheus server after tests (if set-up).")
	flags.BoolEnvVar(&p.ScrapeEtcd, "prometheus-scrape-etcd", "PROMETHEUS_SCRAPE_ETCD", false, "Whether to scrape etcd metrics.")
	flags.BoolEnvVar(&p.ScrapeNodeExporter, "prometheus-scrape-node-exporter", "PROMETHEUS_SCRAPE_NODE_EXPORTER", false, "Whether to scrape node exporter metrics.")
	flags.BoolEnvVar(&p.ScrapeKubelets, "prometheus-scrape-kubelets", "PROMETHEUS_SCRAPE_KUBELETS", false, "Whether to scrape kubelets. Experimental, may not work in larger clusters. Requires heapster node to be at least n1-standard-4, which needs to be provided manually.")
	flags.BoolEnvVar(&p.ScrapeKubeProxy, "prometheus-scrape-kube-proxy", "PROMETHEUS_SCRAPE_KUBE_PROXY", true, "Whether to scrape kube proxy.")
}

// PrometheusController is a util for managing (setting up / tearing down) the prometheus stack in
// the cluster.
type PrometheusController struct {
	clusterLoaderConfig *config.ClusterLoaderConfig
	// provider is the cloud provider derived from the --provider flag.
	provider string
	// framework associated with the cluster where the prometheus stack should be set up.
	// For kubemark it's the root cluster, otherwise it's the main (and only) cluster.
	framework *framework.Framework
	// templateMapping is a mapping defining placeholders used in manifest templates.
	templateMapping map[string]interface{}
	// diskMetadata store name and zone of Prometheus persistent disk.
	diskMetadata prometheusDiskMetadata
}

// NewPrometheusController creates a new instance of PrometheusController for the given config.
func NewPrometheusController(clusterLoaderConfig *config.ClusterLoaderConfig) (pc *PrometheusController, err error) {
	pc = &PrometheusController{
		clusterLoaderConfig: clusterLoaderConfig,
		provider:            clusterLoaderConfig.ClusterConfig.Provider,
	}

	if pc.framework, err = framework.NewRootFramework(&clusterLoaderConfig.ClusterConfig, numK8sClients); err != nil {
		return nil, err
	}

	mapping, errList := config.GetMapping(clusterLoaderConfig)
	if errList != nil {
		return nil, errList
	}
	mapping["MasterIps"], err = getMasterIps(clusterLoaderConfig.ClusterConfig)
	if err != nil {
		klog.Warningf("Couldn't get master ip, will ignore manifests requiring it: %v", err)
		delete(mapping, "MasterIps")
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_APISERVER_ONLY"]; !exists {
		mapping["PROMETHEUS_SCRAPE_APISERVER_ONLY"] = clusterLoaderConfig.ClusterConfig.Provider == "gke" || clusterLoaderConfig.ClusterConfig.Provider == "aks"
	}
	// TODO: Change to pure assignments when overrides are not used.
	if _, exists := mapping["PROMETHEUS_SCRAPE_ETCD"]; !exists {
		mapping["PROMETHEUS_SCRAPE_ETCD"] = clusterLoaderConfig.PrometheusConfig.ScrapeEtcd
	} else {
		// Backward compatibility.
		clusterLoaderConfig.PrometheusConfig.ScrapeEtcd = mapping["PROMETHEUS_SCRAPE_ETCD"].(bool)
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_NODE_EXPORTER"]; !exists {
		mapping["PROMETHEUS_SCRAPE_NODE_EXPORTER"] = clusterLoaderConfig.PrometheusConfig.ScrapeNodeExporter
	} else {
		// Backward compatibility.
		clusterLoaderConfig.PrometheusConfig.ScrapeNodeExporter = mapping["PROMETHEUS_SCRAPE_NODE_EXPORTER"].(bool)
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"]; !exists {
		mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy
	} else {
		// Backward compatibility.
		clusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy = mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"].(bool)
	}
	mapping["PROMETHEUS_SCRAPE_KUBELETS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubelets

	pc.templateMapping = mapping

	return pc, nil
}

// SetUpPrometheusStack sets up prometheus stack in the cluster.
// This method is idempotent, if the prometheus stack is already set up applying the manifests
// again will be no-op.
func (pc *PrometheusController) SetUpPrometheusStack() error {
	k8sClient := pc.framework.GetClientSets().GetClient()

	klog.Info("Setting up prometheus stack")
	if err := client.CreateNamespace(k8sClient, namespace); err != nil {
		return err
	}
	// If enabled scraping windows node, need to setup windows node and template mapping
	if isWindowsNodeScrapingEnabled(pc.templateMapping, pc.clusterLoaderConfig) {
		if err := setUpWindowsNodeAndTemplate(k8sClient, pc.templateMapping); err != nil {
			return err
		}
	}
	if err := pc.applyManifests(coreManifests); err != nil {
		return err
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeNodeExporter {
		if err := pc.runNodeExporter(); err != nil {
			return err
		}
	}
	if !pc.isKubemark() {
		if err := pc.applyManifests(defaultServiceMonitors); err != nil {
			return err
		}
	}

	if _, ok := pc.templateMapping["MasterIps"]; ok {
		if err := pc.exposeAPIServerMetrics(); err != nil {
			return err
		}
		if err := pc.applyManifests(masterIPServiceMonitors); err != nil {
			return err
		}
	}
	if err := pc.waitForPrometheusToBeHealthy(); err != nil {
		dumpAdditionalLogsOnPrometheusSetupFailure(k8sClient)
		return err
	}
	klog.Info("Prometheus stack set up successfully")
	if err := pc.cachePrometheusDiskMetadataIfEnabled(); err != nil {
		klog.Warningf("Error while caching prometheus disk metadata: %v", err)
	}
	return nil
}

// TearDownPrometheusStack tears down prometheus stack, releasing all prometheus resources.
func (pc *PrometheusController) TearDownPrometheusStack() error {
	if err := pc.snapshotPrometheusDiskIfEnabled(); err != nil {
		klog.Warningf("Error while snapshotting prometheus disk: %v", err)
	}
	klog.Info("Tearing down prometheus stack")
	k8sClient := pc.framework.GetClientSets().GetClient()
	if err := client.DeleteNamespace(k8sClient, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(k8sClient, namespace); err != nil {
		return err
	}
	return nil
}

// GetFramework returns prometheus framework.
func (pc *PrometheusController) GetFramework() *framework.Framework {
	return pc.framework
}

func (pc *PrometheusController) applyManifests(manifestGlob string) error {
	return pc.framework.ApplyTemplatedManifests(
		manifestGlob, pc.templateMapping, client.Retry(apierrs.IsNotFound))
}

// exposeAPIServerMetrics configures anonymous access to the apiserver metrics.
func (pc *PrometheusController) exposeAPIServerMetrics() error {
	klog.Info("Exposing kube-apiserver metrics in the cluster")
	// We need to get a client to the cluster where the test is being executed on,
	// not the cluster that the prometheus is running in. Usually, there is only
	// once cluster, but in case of kubemark we have two and thus we need to
	// create a new client here.
	clientSet, err := framework.NewMultiClientSet(
		pc.clusterLoaderConfig.ClusterConfig.KubeConfigPath, numK8sClients)
	if err != nil {
		return err
	}
	createClusterRole := func() error {
		_, err := clientSet.GetClient().RbacV1().ClusterRoles().Create(&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "apiserver-metrics-viewer"},
			Rules: []rbacv1.PolicyRule{
				{Verbs: []string{"get"}, NonResourceURLs: []string{"/metrics"}},
			},
		})
		return err
	}
	createClusterRoleBinding := func() error {
		_, err := clientSet.GetClient().RbacV1().ClusterRoleBindings().Create(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "system:anonymous"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "apiserver-metrics-viewer"},
			Subjects: []rbacv1.Subject{
				{Kind: "User", Name: "system:anonymous"},
			},
		})
		return err
	}
	if err := retryCreateFunction(createClusterRole); err != nil {
		return err
	}
	if err := retryCreateFunction(createClusterRoleBinding); err != nil {
		return err
	}
	return nil
}

// runNodeExporter adds node-exporter as master's static manifest pod.
// TODO(mborsz): Consider migrating to something less ugly, e.g. daemonset-based approach,
// when master nodes have configured networking.
func (pc *PrometheusController) runNodeExporter() error {
	klog.Infof("Starting node-exporter on master nodes.")
	kubemarkFramework, err := framework.NewFramework(&pc.clusterLoaderConfig.ClusterConfig, numK8sClients)
	if err != nil {
		return err
	}

	// Validate masters first
	nodes, err := client.ListNodes(kubemarkFramework.GetClientSets().GetClient())
	if err != nil {
		return err
	}

	var g errgroup.Group
	numMasters := 0
	for _, node := range nodes {
		node := node
		if system.IsMasterNode(node.Name) {
			numMasters++
			g.Go(func() error {
				f, err := os.Open(os.ExpandEnv(nodeExporterPod))
				if err != nil {
					return fmt.Errorf("Unable to open manifest file: %v", err)
				}
				defer f.Close()
				return util.SSH("sudo tee /etc/kubernetes/manifests/node-exporter.yaml > /dev/null", &node, f)
			})
		}
	}

	if numMasters == 0 {
		return fmt.Errorf("node-exporter requires master to be registered nodes")
	}

	return g.Wait()
}

func (pc *PrometheusController) waitForPrometheusToBeHealthy() error {
	klog.Info("Waiting for Prometheus stack to become healthy...")
	return wait.Poll(
		checkPrometheusReadyInterval,
		checkPrometheusReadyTimeout,
		pc.isPrometheusReady)
}

func (pc *PrometheusController) isPrometheusReady() (bool, error) {
	// TODO(mm4tt): Re-enable kube-proxy monitoring and expect more targets.
	// This is a safeguard from a race condition where the prometheus server is started before
	// targets are registered. These 4 targets are always expected, in all possible configurations:
	// prometheus, prometheus-operator, grafana, apiserver
	expectedTargets := 4
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeEtcd {
		// If scraping etcd is enabled (or it's kubemark where we scrape etcd unconditionally) we need
		// a bit more complicated logic to asses whether all targets are ready. Etcd metric port has
		// changed in https://github.com/kubernetes/kubernetes/pull/77561, depending on the k8s version
		// etcd metrics may be available at port 2379 xor 2382. We solve that by setting two etcd
		// serviceMonitors one for 2379 and other for 2382 and expect that at least 1 of them should be healthy.
		ok, err := CheckAllTargetsReady( // All non-etcd targets should be ready.
			pc.framework.GetClientSets().GetClient(),
			func(t Target) bool { return !isEtcdEndpoint(t.Labels["endpoint"]) },
			expectedTargets)
		if err != nil || !ok {
			return ok, err
		}
		return CheckTargetsReady( // 1 out of 2 etcd targets should be ready.
			pc.framework.GetClientSets().GetClient(),
			func(t Target) bool { return isEtcdEndpoint(t.Labels["endpoint"]) },
			2, // expected targets: etcd-2379 and etcd-2382
			1) // one of them should be healthy
	}
	return CheckAllTargetsReady(
		pc.framework.GetClientSets().GetClient(),
		func(Target) bool { return true }, // All targets.
		expectedTargets)
}

func retryCreateFunction(f func() error) error {
	return client.RetryWithExponentialBackOff(
		client.RetryFunction(f, client.Allow(apierrs.IsAlreadyExists)))
}

func (pc *PrometheusController) isKubemark() bool {
	return pc.provider == "kubemark"
}

func dumpAdditionalLogsOnPrometheusSetupFailure(k8sClient kubernetes.Interface) {
	klog.Info("Dumping monitoring/prometheus-k8s events...")
	list, err := client.ListEvents(k8sClient, namespace, "prometheus-k8s")
	if err != nil {
		klog.Warningf("Error while listing monitoring/prometheus-k8s events: %v", err)
		return
	}
	s, err := json.MarshalIndent(list, "" /*=prefix*/, "  " /*=indent*/)
	if err != nil {
		klog.Warningf("Error while marshalling response %v: %v", list, err)
		return
	}
	klog.Info(string(s))
}

func getMasterIps(clusterConfig config.ClusterConfig) ([]string, error) {
	if len(clusterConfig.MasterInternalIPs) != 0 {
		klog.Infof("Using internal master ips (%s) to monitor master's components", clusterConfig.MasterInternalIPs)
		return clusterConfig.MasterInternalIPs, nil
	}
	klog.Infof("Unable to determine master ips from flags or registered nodes. Will fallback to default/kubernetes service, which can be inaccurate in HA environments.")
	ips, err := getMasterIpsFromKubernetesService(clusterConfig)
	if err != nil {
		klog.Warningf("Failed to translate default/kubernetes service to IP: %v", err)
		return nil, fmt.Errorf("no ips are set, fallback to default/kubernetes service failed due to: %v", err)
	}
	klog.Infof("default/kubernetes service translated to: %v", ips)
	return ips, nil
}

func getMasterIpsFromKubernetesService(clusterConfig config.ClusterConfig) ([]string, error) {
	// This has to be done in the kubemark cluster, thus we need to create a new client.
	clientSet, err := framework.NewMultiClientSet(clusterConfig.KubeConfigPath, numK8sClients)
	if err != nil {
		return nil, err
	}

	var endpoints *corev1.Endpoints
	f := func() error {
		var err error
		endpoints, err = clientSet.GetClient().CoreV1().Endpoints("default").Get("kubernetes", metav1.GetOptions{})
		return err
	}

	if err := client.RetryWithExponentialBackOff(client.RetryFunction(f)); err != nil {
		return nil, err
	}

	var ips []string
	for _, subnet := range endpoints.Subsets {
		for _, address := range subnet.Addresses {
			ips = append(ips, address.IP)
		}
	}

	if len(ips) == 0 {
		return nil, errors.New("no master ips available in default/kubernetes service")
	}

	return ips, nil
}

func isEtcdEndpoint(endpoint string) bool {
	return endpoint == "etcd-2379" || endpoint == "etcd-2382"
}
