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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	clerrors "k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	namespace                    = "monitoring"
	storageClass                 = "ssd"
	checkPrometheusReadyInterval = 30 * time.Second
	numK8sClients                = 1
)

// InitFlags initializes prometheus flags.
func InitFlags(p *config.PrometheusConfig) {
	flags.BoolEnvVar(&p.EnableServer, "enable-prometheus-server", "ENABLE_PROMETHEUS_SERVER", false, "Whether to set-up the prometheus server in the cluster.")
	flags.BoolEnvVar(&p.TearDownServer, "tear-down-prometheus-server", "TEAR_DOWN_PROMETHEUS_SERVER", true, "Whether to tear-down the prometheus server after tests (if set-up).")
	flags.BoolEnvVar(&p.EnablePushgateway, "enable-pushgateway", "PROMETHEUS_ENABLE_PUSHGATEWAY", false, "Whether to set-up the Pushgateway. Only work with enabled Prometheus server.")
	flags.BoolEnvVar(&p.ScrapeEtcd, "prometheus-scrape-etcd", "PROMETHEUS_SCRAPE_ETCD", false, "Whether to scrape etcd metrics.")
	flags.BoolEnvVar(&p.ScrapeNodeExporter, "prometheus-scrape-node-exporter", "PROMETHEUS_SCRAPE_NODE_EXPORTER", false, "Whether to scrape node exporter metrics.")
	flags.BoolEnvVar(&p.ScrapeWindowsNodeExporter, "prometheus-scrape-windows-node-exporter", "PROMETHEUS_SCRAPE_WINDOWS_NODE_EXPORTER", false, "Whether to scrape Windows node exporter metrics.")
	flags.BoolEnvVar(&p.ScrapeKubelets, "prometheus-scrape-kubelets", "PROMETHEUS_SCRAPE_KUBELETS", false, "Whether to scrape kubelets (nodes + master). Experimental, may not work in larger clusters. Requires heapster node to be at least n1-standard-4, which needs to be provided manually.")
	flags.BoolEnvVar(&p.ScrapeMasterKubelets, "prometheus-scrape-master-kubelets", "PROMETHEUS_SCRAPE_MASTER_KUBELETS", false, "Whether to scrape kubelets running on master nodes.")
	flags.BoolEnvVar(&p.ScrapeKubeProxy, "prometheus-scrape-kube-proxy", "PROMETHEUS_SCRAPE_KUBE_PROXY", true, "Whether to scrape kube proxy.")
	flags.BoolEnvVar(&p.ScrapeKubeStateMetrics, "prometheus-scrape-kube-state-metrics", "PROMETHEUS_SCRAPE_KUBE_STATE_METRICS", false, "Whether to scrape kube-state-metrics. Only run occasionally.")
	flags.BoolEnvVar(&p.ScrapeMetricsServerMetrics, "prometheus-scrape-metrics-server", "PROMETHEUS_SCRAPE_METRICS_SERVER_METRICS", false, "Whether to scrape metrics-server. Only run occasionally.")
	flags.BoolEnvVar(&p.ScrapeNodeLocalDNS, "prometheus-scrape-node-local-dns", "PROMETHEUS_SCRAPE_NODE_LOCAL_DNS", false, "Whether to scrape node-local-dns pods.")
	flags.BoolEnvVar(&p.ScrapeAnet, "prometheus-scrape-anet", "PROMETHEUS_SCRAPE_ANET", false, "Whether to scrape anet pods.")
	flags.BoolEnvVar(&p.ScrapeCiliumOperator, "prometheus-scrape-cilium-operator", "PROMETHEUS_SCRAPE_CILIUM_OPERATOR", false, "Whether to scrape cilium-operator pods.")
	flags.IntEnvVar(&p.APIServerScrapePort, "prometheus-apiserver-scrape-port", "PROMETHEUS_APISERVER_SCRAPE_PORT", 443, "Port for scraping kube-apiserver (default 443).")
	flags.StringEnvVar(&p.SnapshotProject, "experimental-snapshot-project", "PROJECT", "", "GCP project used where disks and snapshots are located.")
	flags.StringEnvVar(&p.ManifestPath, "prometheus-manifest-path", "PROMETHEUS_MANIFEST_PATH", "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests", "Path to the prometheus manifest files.")
	flags.StringEnvVar(&p.StorageClassProvisioner, "prometheus-storage-class-provisioner", "PROMETHEUS_STORAGE_CLASS_PROVISIONER", "kubernetes.io/gce-pd", "Volumes plugin used to provision PVs for Prometheus.")
	flags.StringEnvVar(&p.StorageClassVolumeType, "prometheus-storage-class-volume-type", "PROMETHEUS_STORAGE_CLASS_VOLUME_TYPE", "pd-ssd", "Volume types of storage class, This will be different depending on the provisioner.")
	flags.DurationEnvVar(&p.ReadyTimeout, "prometheus-ready-timeout", "PROMETHEUS_READY_TIMEOUT", 15*time.Minute, "Timeout for waiting for Prometheus stack to become healthy.")
}

// ValidatePrometheusFlags validates prometheus flags.
func ValidatePrometheusFlags(p *config.PrometheusConfig) *clerrors.ErrorList {
	errList := clerrors.NewErrorList()
	if *shouldSnapshotPrometheusDisk && p.SnapshotProject == "" {
		errList.Append(fmt.Errorf("requesting snapshot, but snapshot project not configured. Use --experimental-snapshot-project flag"))
	}
	return errList
}

// Controller is a util for managing (setting up / tearing down) the prometheus stack in
// the cluster.
type Controller struct {
	clusterLoaderConfig *config.ClusterLoaderConfig
	// provider is the cloud provider derived from the --provider flag.
	provider provider.Provider
	// framework associated with the cluster where the prometheus stack should be set up.
	// For kubemark it's the root cluster, otherwise it's the main (and only) cluster.
	framework *framework.Framework
	// templateMapping is a mapping defining placeholders used in manifest templates.
	templateMapping map[string]interface{}
	// diskMetadata store name and zone of Prometheus persistent disk.
	diskMetadata prometheusDiskMetadata
	// snapshotLock makes sure that only single Prometheus snapshot is happening
	snapshotLock sync.Mutex
	// snapshotted is a check if the Prometheus snapshot is already done - protected by snapshotLock
	snapshotted bool
	// snapshotError contains error from snapshot attempt - protected by snapshotLock
	snapshotError error
	// ssh executor to run commands in cluster nodes via ssh
	ssh util.SSHExecutor
	// timeout for waiting for Prometheus stack to become healthy
	readyTimeout time.Duration
}

// CompleteConfig completes Prometheus manifest file path config
func CompleteConfig(p *config.PrometheusConfig) {
	p.CoreManifests = p.ManifestPath + "/*.yaml"
	p.DefaultServiceMonitors = p.ManifestPath + "/default/*.yaml"
	p.KubeStateMetricsManifests = p.ManifestPath + "/exporters/kube-state-metrics/*.yaml"
	p.MasterIPServiceMonitors = p.ManifestPath + "/master-ip/*.yaml"
	p.MetricsServerManifests = p.ManifestPath + "/exporters/metrics-server/*.yaml"
	p.NodeExporterPod = p.ManifestPath + "/exporters/node_exporter/node-exporter.yaml"
	p.WindowsNodeExporterManifests = p.ManifestPath + "/exporters/windows_node_exporter/*.yaml"
	p.PushgatewayManifests = p.ManifestPath + "/pushgateway/*.yaml"
}

// NewController creates a new instance of Controller for the given config.
func NewController(clusterLoaderConfig *config.ClusterLoaderConfig) (pc *Controller, err error) {
	pc = &Controller{
		clusterLoaderConfig: clusterLoaderConfig,
		provider:            clusterLoaderConfig.ClusterConfig.Provider,
		readyTimeout:        clusterLoaderConfig.PrometheusConfig.ReadyTimeout,
	}

	if pc.framework, err = framework.NewRootFramework(&clusterLoaderConfig.ClusterConfig, numK8sClients); err != nil {
		return nil, err
	}

	mapping, errList := config.GetMapping(clusterLoaderConfig, nil)
	if errList != nil {
		return nil, errList
	}
	mapping["MasterIps"], err = getMasterIps(clusterLoaderConfig.ClusterConfig)
	if err != nil {
		klog.Warningf("Couldn't get master ip, will ignore manifests requiring it: %v", err)
		delete(mapping, "MasterIps")
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_APISERVER_ONLY"]; !exists {
		mapping["PROMETHEUS_SCRAPE_APISERVER_ONLY"] = clusterLoaderConfig.ClusterConfig.Provider.Features().ShouldPrometheusScrapeApiserverOnly
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
	if _, exists := mapping["PROMETHEUS_SCRAPE_WINDOWS_NODE_EXPORTER"]; !exists {
		mapping["PROMETHEUS_SCRAPE_WINDOWS_NODE_EXPORTER"] = clusterLoaderConfig.PrometheusConfig.ScrapeWindowsNodeExporter
	} else {
		// Backward compatibility.
		clusterLoaderConfig.PrometheusConfig.ScrapeWindowsNodeExporter = mapping["PROMETHEUS_SCRAPE_WINDOWS_NODE_EXPORTER"].(bool)
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"]; !exists {
		clusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy = clusterLoaderConfig.ClusterConfig.Provider.Features().ShouldScrapeKubeProxy
		mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy
	} else {
		// Backward compatibility
		clusterLoaderConfig.PrometheusConfig.ScrapeKubeProxy = mapping["PROMETHEUS_SCRAPE_KUBE_PROXY"].(bool)
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_ANET"]; !exists {
		mapping["PROMETHEUS_SCRAPE_ANET"] = clusterLoaderConfig.PrometheusConfig.ScrapeAnet
	} else {
		clusterLoaderConfig.PrometheusConfig.ScrapeAnet = mapping["PROMETHEUS_SCRAPE_ANET"].(bool)
	}
	if _, exists := mapping["PROMETHEUS_SCRAPE_CILIUM_OPERATOR"]; !exists {
		mapping["PROMETHEUS_SCRAPE_CILIUM_OPERATOR"] = clusterLoaderConfig.PrometheusConfig.ScrapeCiliumOperator
	} else {
		clusterLoaderConfig.PrometheusConfig.ScrapeCiliumOperator = mapping["PROMETHEUS_SCRAPE_CILIUM_OPERATOR"].(bool)
	}
	mapping["PROMETHEUS_SCRAPE_NODE_LOCAL_DNS"] = clusterLoaderConfig.PrometheusConfig.ScrapeNodeLocalDNS
	mapping["PROMETHEUS_SCRAPE_KUBE_STATE_METRICS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubeStateMetrics
	mapping["PROMETHEUS_SCRAPE_METRICS_SERVER_METRICS"] = clusterLoaderConfig.PrometheusConfig.ScrapeMetricsServerMetrics
	mapping["PROMETHEUS_SCRAPE_KUBELETS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubelets
	mapping["PROMETHEUS_SCRAPE_MASTER_KUBELETS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubelets || clusterLoaderConfig.PrometheusConfig.ScrapeMasterKubelets
	mapping["PROMETHEUS_APISERVER_SCRAPE_PORT"] = clusterLoaderConfig.PrometheusConfig.APIServerScrapePort
	mapping["PROMETHEUS_STORAGE_CLASS_PROVISIONER"] = clusterLoaderConfig.PrometheusConfig.StorageClassProvisioner
	mapping["PROMETHEUS_STORAGE_CLASS_VOLUME_TYPE"] = clusterLoaderConfig.PrometheusConfig.StorageClassVolumeType
	snapshotEnabled, _ := pc.isEnabled()
	mapping["RetainPD"] = snapshotEnabled

	pc.templateMapping = mapping

	pc.ssh = &util.GCloudSSHExecutor{}

	return pc, nil
}

// SetUpPrometheusStack sets up prometheus stack in the cluster.
// This method is idempotent, if the prometheus stack is already set up applying the manifests
// again will be no-op.
func (pc *Controller) SetUpPrometheusStack() error {
	k8sClient := pc.framework.GetClientSets().GetClient()

	klog.V(2).Info("Setting up prometheus stack")
	if err := client.CreateNamespace(k8sClient, namespace); err != nil {
		return err
	}
	// Removing Storage Class as Reclaim Policy cannot be changed
	if err := client.DeleteStorageClass(k8sClient, storageClass); err != nil {
		return err
	}
	if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.CoreManifests); err != nil {
		return err
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeNodeExporter {
		if err := pc.runNodeExporter(); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeWindowsNodeExporter {
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.WindowsNodeExporterManifests); err != nil {
			return err
		}
	} else {
		// Backward compatibility
		// If enabled scraping windows node, need to setup windows node and template mapping
		if isWindowsNodeScrapingEnabled(pc.templateMapping, pc.clusterLoaderConfig) {
			if err := setUpWindowsNodeAndTemplate(k8sClient, pc.templateMapping); err != nil {
				return err
			}
		}
	}
	if !pc.isKubemark() {
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.DefaultServiceMonitors); err != nil {
			return err
		}
	}

	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeKubeStateMetrics && pc.clusterLoaderConfig.ClusterConfig.Provider.Features().SupportKubeStateMetrics {
		klog.V(2).Infof("Applying kube-state-metrics in the cluster.")
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.KubeStateMetricsManifests); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeMetricsServerMetrics && pc.clusterLoaderConfig.ClusterConfig.Provider.Features().SupportMetricsServerMetrics {
		klog.V(2).Infof("Applying metrics server in the cluster.")
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.MetricsServerManifests); err != nil {
			return err
		}
	}
	if _, ok := pc.templateMapping["MasterIps"]; ok {
		if err := pc.exposeAPIServerMetrics(); err != nil {
			return err
		}
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.MasterIPServiceMonitors); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.EnablePushgateway {
		klog.V(2).Infof("Applying Pushgateway in the cluster.")
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.PushgatewayManifests); err != nil {
			return err
		}
	}
	if err := pc.waitForPrometheusToBeHealthy(); err != nil {
		dumpAdditionalLogsOnPrometheusSetupFailure(k8sClient)
		return err
	}
	klog.V(2).Info("Prometheus stack set up successfully")
	if err := pc.cachePrometheusDiskMetadataIfEnabled(); err != nil {
		klog.Warningf("Error while caching prometheus disk metadata: %v", err)
	}
	return nil
}

func (pc *Controller) MakePrometheusSnapshotIfEnabled() error {
	klog.V(2).Info("Get snapshot from Prometheus")
	if err := pc.snapshotPrometheusIfEnabled(); err != nil {
		klog.Warningf("Error while getting prometheus snapshot: %v", err)
		return err
	}

	return nil
}

// TearDownPrometheusStack tears down prometheus stack, releasing all prometheus resources.
func (pc *Controller) TearDownPrometheusStack() error {
	// Get disk metadata again to be sure
	if err := pc.cachePrometheusDiskMetadataIfEnabled(); err != nil {
		klog.Warningf("Error while caching prometheus disk metadata: %v", err)
	}
	defer func() {
		klog.V(2).Info("Snapshotting prometheus disk")
		if err := pc.snapshotPrometheusDiskIfEnabledSynchronized(); err != nil {
			klog.Warningf("Error while snapshotting prometheus disk: %v", err)
		}
		if err := pc.deletePrometheusDiskIfEnabled(); err != nil {
			klog.Warningf("Error while deleting prometheus disk: %v", err)
		}
	}()

	klog.V(2).Info("Tearing down prometheus stack")
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
func (pc *Controller) GetFramework() *framework.Framework {
	return pc.framework
}

func (pc *Controller) applyManifests(manifestGlob string) error {
	return pc.framework.ApplyTemplatedManifests(
		manifestGlob, pc.templateMapping, client.Retry(apierrs.IsNotFound))
}

// exposeAPIServerMetrics configures anonymous access to the apiserver metrics.
func (pc *Controller) exposeAPIServerMetrics() error {
	klog.V(2).Info("Exposing kube-apiserver metrics in the cluster")
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
		_, err := clientSet.GetClient().RbacV1().ClusterRoles().Create(context.TODO(), &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "apiserver-metrics-viewer"},
			Rules: []rbacv1.PolicyRule{
				{Verbs: []string{"get"}, NonResourceURLs: []string{"/metrics"}},
			},
		}, metav1.CreateOptions{})
		return err
	}
	createClusterRoleBinding := func() error {
		_, err := clientSet.GetClient().RbacV1().ClusterRoleBindings().Create(context.TODO(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "system:anonymous"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "apiserver-metrics-viewer"},
			Subjects: []rbacv1.Subject{
				{Kind: "User", Name: "system:anonymous"},
			},
		}, metav1.CreateOptions{})
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
func (pc *Controller) runNodeExporter() error {
	klog.V(2).Infof("Starting node-exporter on master nodes.")
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
		if util.LegacyIsMasterNode(&node) {
			numMasters++
			g.Go(func() error {
				f, err := os.Open(os.ExpandEnv(pc.clusterLoaderConfig.PrometheusConfig.NodeExporterPod))
				if err != nil {
					return fmt.Errorf("unable to open manifest file: %v", err)
				}
				defer f.Close()
				return pc.ssh.Exec("sudo tee /etc/kubernetes/manifests/node-exporter.yaml > /dev/null", &node, f)
			})
		}
	}

	if numMasters == 0 {
		return fmt.Errorf("node-exporter requires master to be registered nodes")
	}

	return g.Wait()
}

func (pc *Controller) waitForPrometheusToBeHealthy() error {
	klog.V(2).Info("Waiting for Prometheus stack to become healthy...")
	return wait.Poll(
		checkPrometheusReadyInterval,
		pc.readyTimeout,
		pc.isPrometheusReady)
}

func (pc *Controller) isPrometheusReady() (bool, error) {
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

func (pc *Controller) isKubemark() bool {
	return pc.provider.Features().IsKubemarkProvider
}

func dumpAdditionalLogsOnPrometheusSetupFailure(k8sClient kubernetes.Interface) {
	klog.V(2).Info("Dumping monitoring/prometheus-k8s events...")
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
	klog.V(2).Info(string(s))
}

func getMasterIps(clusterConfig config.ClusterConfig) ([]string, error) {
	if len(clusterConfig.MasterInternalIPs) != 0 {
		klog.V(2).Infof("Using internal master ips (%s) to monitor master's components", clusterConfig.MasterInternalIPs)
		return clusterConfig.MasterInternalIPs, nil
	}
	klog.V(1).Infof("Unable to determine master ips from flags or registered nodes. Will fallback to default/kubernetes service, which can be inaccurate in HA environments.")
	ips, err := getMasterIpsFromKubernetesService(clusterConfig)
	if err != nil {
		klog.Warningf("Failed to translate default/kubernetes service to IP: %v", err)
		return nil, fmt.Errorf("no ips are set, fallback to default/kubernetes service failed due to: %v", err)
	}
	klog.V(2).Infof("default/kubernetes service translated to: %v", ips)
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
		endpoints, err = clientSet.GetClient().CoreV1().Endpoints("default").Get(context.TODO(), "kubernetes", metav1.GetOptions{})
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
