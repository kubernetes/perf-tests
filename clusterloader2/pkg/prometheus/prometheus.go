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
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
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
	monitoringServiceAccount     = "monitoringserviceaccount"
	secretName                   = "prometheus-token"

	// All paths here are relative to manifests dir.
	coreManifests                = "*.yaml"
	defaultServiceMonitors       = "default/*.yaml"
	kubeStateMetricsManifests    = "exporters/kube-state-metrics/*.yaml"
	masterIPServiceMonitors      = "master-ip/*.yaml"
	metricsServerManifests       = "exporters/metrics-server/*.yaml"
	nodeExporterPod              = "exporters/node_exporter/node-exporter.yaml"
	windowsNodeExporterManifests = "exporters/windows_node_exporter/*.yaml"
	pushgatewayManifests         = "pushgateway/*.yaml"
)

//go:embed manifests
var manifestsFSWithPrefix embed.FS
var manifestsFS fs.FS

func init() {
	var err error
	// go's embed generates embed.FS with all files with 'manifests/' prefix.
	// To be consistent with --prometheus-manifest-path (which is defined inside of manifests) we need to drip this prefix.
	manifestsFS, err = fs.Sub(manifestsFSWithPrefix, "manifests")
	if err != nil {
		panic(fmt.Sprintf("failed to strip manifests prefix: %v", err))
	}
}

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
	flags.StringEnvVar(&p.KubeProxySelectorKey, "prometheus-kube-proxy-selector-key", "PROMETHEUS_KUBE_PROXY_SELECTOR_KEY", "component", "Label key used to scrape kube proxy.")
	flags.BoolEnvVar(&p.ScrapeKubeStateMetrics, "prometheus-scrape-kube-state-metrics", "PROMETHEUS_SCRAPE_KUBE_STATE_METRICS", false, "Whether to scrape kube-state-metrics. Only run occasionally.")
	flags.BoolEnvVar(&p.ScrapeMetricsServerMetrics, "prometheus-scrape-metrics-server", "PROMETHEUS_SCRAPE_METRICS_SERVER_METRICS", false, "Whether to scrape metrics-server. Only run occasionally.")
	flags.BoolEnvVar(&p.ScrapeNodeLocalDNS, "prometheus-scrape-node-local-dns", "PROMETHEUS_SCRAPE_NODE_LOCAL_DNS", false, "Whether to scrape node-local-dns pods.")
	flags.BoolEnvVar(&p.ScrapeAnet, "prometheus-scrape-anet", "PROMETHEUS_SCRAPE_ANET", false, "Whether to scrape anet pods.")
	flags.BoolEnvVar(&p.ScrapeNetworkPolicies, "prometheus-scrape-kube-network-policies", "PROMETHEUS_SCRAPE_KUBE_NETWORK_POLICIES", false, "Whether to scrape kube-network-policies pods.")
	flags.BoolEnvVar(&p.ScrapeMastersWithPublicIPs, "prometheus-scrape-masters-with-public-ips", "PROMETHEUS_SCRAPE_MASTERS_WITH_PUBLIC_IPS", false, "Whether to scrape master machines using public ips, instead of private.")
	flags.IntEnvVar(&p.APIServerScrapePort, "prometheus-apiserver-scrape-port", "PROMETHEUS_APISERVER_SCRAPE_PORT", 443, "Port for scraping kube-apiserver (default 443).")
	flags.StringEnvVar(&p.SnapshotProject, "experimental-snapshot-project", "PROJECT", "", "GCP project used where disks and snapshots are located.")
	flags.StringEnvVar(&p.AdditionalMonitorsPath, "prometheus-additional-monitors-path", "PROMETHEUS_ADDITIONAL_MONITORS_PATH", "", "Additional monitors to apply.")
	flags.StringEnvVar(&p.StorageClassProvisioner, "prometheus-storage-class-provisioner", "PROMETHEUS_STORAGE_CLASS_PROVISIONER", "kubernetes.io/gce-pd", "Volumes plugin used to provision PVs for Prometheus.")
	flags.StringEnvVar(&p.StorageClassVolumeType, "prometheus-storage-class-volume-type", "PROMETHEUS_STORAGE_CLASS_VOLUME_TYPE", "pd-ssd", "Volume types of storage class, This will be different depending on the provisioner.")
	flags.StringEnvVar(&p.PVCStorageClass, "prometheus-pvc-storage-class", "PROMETHEUS_PVC_STORAGE_CLASS", "ssd", "Storage class used with prometheus persistent volume claim.")
	flags.DurationEnvVar(&p.ReadyTimeout, "prometheus-ready-timeout", "PROMETHEUS_READY_TIMEOUT", 15*time.Minute, "Timeout for waiting for Prometheus stack to become healthy.")
	flags.StringEnvVar(&p.PrometheusMemoryRequest, "prometheus-memory-request", "PROMETHEUS_MEMORY_REQUEST", "10Gi", "Memory request to be used by promehteus.")
}

// ValidatePrometheusFlags validates prometheus flags.
func ValidatePrometheusFlags(p *config.PrometheusConfig) *clerrors.ErrorList {
	errList := clerrors.NewErrorList()
	if shouldSnapshotPrometheusDisk && p.SnapshotProject == "" {
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
	mapping["MasterIps"], err = getMasterIps(clusterLoaderConfig.ClusterConfig, clusterLoaderConfig.PrometheusConfig.ScrapeMastersWithPublicIPs)
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
	if _, exists := mapping["PROMETHEUS_SCRAPE_KUBE_NETWORK_POLICIES"]; !exists {
		mapping["PROMETHEUS_SCRAPE_KUBE_NETWORK_POLICIES"] = clusterLoaderConfig.PrometheusConfig.ScrapeNetworkPolicies
	} else {
		clusterLoaderConfig.PrometheusConfig.ScrapeNetworkPolicies = mapping["PROMETHEUS_SCRAPE_KUBE_NETWORK_POLICIES"].(bool)
	}
	mapping["PROMETHEUS_SCRAPE_NODE_LOCAL_DNS"] = clusterLoaderConfig.PrometheusConfig.ScrapeNodeLocalDNS
	mapping["PROMETHEUS_SCRAPE_KUBE_STATE_METRICS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubeStateMetrics
	mapping["PROMETHEUS_SCRAPE_METRICS_SERVER_METRICS"] = clusterLoaderConfig.PrometheusConfig.ScrapeMetricsServerMetrics
	mapping["PROMETHEUS_SCRAPE_KUBELETS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubelets
	mapping["PROMETHEUS_SCRAPE_MASTER_KUBELETS"] = clusterLoaderConfig.PrometheusConfig.ScrapeKubelets || clusterLoaderConfig.PrometheusConfig.ScrapeMasterKubelets
	mapping["PROMETHEUS_APISERVER_SCRAPE_PORT"] = clusterLoaderConfig.PrometheusConfig.APIServerScrapePort
	mapping["PROMETHEUS_STORAGE_CLASS_PROVISIONER"] = clusterLoaderConfig.PrometheusConfig.StorageClassProvisioner
	mapping["PROMETHEUS_STORAGE_CLASS_VOLUME_TYPE"] = clusterLoaderConfig.PrometheusConfig.StorageClassVolumeType
	mapping["PROMETHEUS_KUBE_PROXY_SELECTOR_KEY"] = clusterLoaderConfig.PrometheusConfig.KubeProxySelectorKey
	mapping["PROMETHEUS_PVC_STORAGE_CLASS"] = clusterLoaderConfig.PrometheusConfig.PVCStorageClass
	mapping["PROMETHEUS_MEMORY_REQUEST"] = clusterLoaderConfig.PrometheusConfig.PrometheusMemoryRequest
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
	rootClusterClientSet := pc.framework.GetClientSets().GetClient()

	// In a Kubemark environment, there are two distinct clusters: the root cluster and the test cluster.
	// Therefore, a testClusterClientSet operating on the test cluster is required.
	// Prometheus runs in the root cluster, but certain actions must be performed in the test cluster
	// for which this testClusterClientSet will be used.
	testClusterClientSet, err := func() (kubernetes.Interface, error) {
		multiClientSet, err := framework.NewMultiClientSet(pc.clusterLoaderConfig.ClusterConfig.KubeConfigPath, numK8sClients)
		if err != nil {
			return nil, err
		}
		return multiClientSet.GetClient(), nil
	}()

	if err != nil {
		return err
	}

	klog.V(2).Info("Setting up prometheus stack")
	if err := client.CreateNamespace(rootClusterClientSet, namespace); err != nil {
		return err
	}

	if err := pc.createToken(rootClusterClientSet, testClusterClientSet); err != nil {
		return err
	}

	// Removing Storage Class as Reclaim Policy cannot be changed
	if err := client.DeleteStorageClass(rootClusterClientSet, storageClass); err != nil {
		return err
	}
	if err := pc.applyDefaultManifests(coreManifests); err != nil {
		return err
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeNodeExporter {
		if err := pc.runNodeExporter(); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeWindowsNodeExporter {
		if err := pc.applyDefaultManifests(windowsNodeExporterManifests); err != nil {
			return err
		}
	} else {
		// Backward compatibility
		// If enabled scraping windows node, need to setup windows node and template mapping
		if isWindowsNodeScrapingEnabled(pc.templateMapping, pc.clusterLoaderConfig) {
			if err := setUpWindowsNodeAndTemplate(rootClusterClientSet, pc.templateMapping); err != nil {
				return err
			}
		}
	}
	if !pc.isKubemark() {
		if err := pc.applyDefaultManifests(defaultServiceMonitors); err != nil {
			return err
		}
	}

	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeKubeStateMetrics && pc.clusterLoaderConfig.ClusterConfig.Provider.Features().SupportKubeStateMetrics {
		klog.V(2).Infof("Applying kube-state-metrics in the cluster.")
		if err := pc.applyDefaultManifests(kubeStateMetricsManifests); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.ScrapeMetricsServerMetrics && pc.clusterLoaderConfig.ClusterConfig.Provider.Features().SupportMetricsServerMetrics {
		klog.V(2).Infof("Applying metrics server in the cluster.")
		if err := pc.applyDefaultManifests(metricsServerManifests); err != nil {
			return err
		}
	}
	if _, ok := pc.templateMapping["MasterIps"]; ok {
		if err := pc.configureRBACForMetrics(testClusterClientSet); err != nil {
			return err
		}
		if err := pc.applyDefaultManifests(masterIPServiceMonitors); err != nil {
			return err
		}
	}
	if pc.clusterLoaderConfig.PrometheusConfig.EnablePushgateway {
		klog.V(2).Infof("Applying Pushgateway in the cluster.")
		if err := pc.applyDefaultManifests(pushgatewayManifests); err != nil {
			return err
		}
	}

	if pc.clusterLoaderConfig.PrometheusConfig.AdditionalMonitorsPath != "" {
		klog.V(2).Infof("Applying additional monitors in the cluster.")
		if err := pc.applyManifests(pc.clusterLoaderConfig.PrometheusConfig.AdditionalMonitorsPath, "*.yaml"); err != nil {
			return err
		}
	}
	if err := pc.waitForPrometheusToBeHealthy(); err != nil {
		dumpAdditionalLogsOnPrometheusSetupFailure(rootClusterClientSet)
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
	if err := client.WaitForDeleteNamespace(k8sClient, namespace, client.DefaultNamespaceDeletionTimeout); err != nil {
		return err
	}
	return nil
}

// GetFramework returns prometheus framework.
func (pc *Controller) GetFramework() *framework.Framework {
	return pc.framework
}

func (pc *Controller) applyDefaultManifests(manifestGlob string) error {
	return pc.framework.ApplyTemplatedManifests(
		manifestsFS, manifestGlob, pc.templateMapping, client.Retry(apierrs.IsNotFound))
}

func (pc *Controller) applyManifests(path, manifestGlob string) error {
	return pc.framework.ApplyTemplatedManifests(
		os.DirFS(path), manifestGlob, pc.templateMapping, client.Retry(apierrs.IsNotFound))
}

// In a Kubemark environment, Prometheus runs in the root cluster, while the simulated nodes reside in a separate test cluster.
// Consequently, Prometheus cannot natively scrape metrics from the test cluster due to cross-cluster limitations.
// To address this, this function performs the following:
// 1. Creates a ServiceAccount and its associated token within the test cluster.
// 2. Stores the generated token as a secret in the root cluster.
// Prometheus in the root cluster is configured to use this secret for authenticating metric scraping requests from the test cluster.
// This allows Prometheus to successfully collect metrics from the simulated nodes in the Kubemark test cluster.
func (pc *Controller) createToken(k8sClient kubernetes.Interface, testClusterClientSet kubernetes.Interface) error {
	klog.V(2).Info("Creating ServiceAccount in testing cluster")

	saObj := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringServiceAccount,
		},
	}

	token := func() (string, error) {
		expirationSeconds := int64(86400) // 24h
		tokenReq := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: &expirationSeconds,
			},
		}
		tokenResp, err := testClusterClientSet.CoreV1().ServiceAccounts(corev1.NamespaceDefault).CreateToken(context.TODO(), saObj.Name, tokenReq, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to create token: %v", err)
		}
		if len(tokenResp.Status.Token) == 0 {
			return "", fmt.Errorf("failed to create token: no token in server response")
		}
		return tokenResp.Status.Token, nil
	}

	secret := func(token string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
			Type: corev1.SecretTypeOpaque,
		}
	}

	serviceAccount := func() error {
		_, err := testClusterClientSet.CoreV1().ServiceAccounts(corev1.NamespaceDefault).Create(context.TODO(), saObj, metav1.CreateOptions{})
		return err
	}

	// Check if the service account already exists
	_, err := testClusterClientSet.CoreV1().ServiceAccounts(corev1.NamespaceDefault).Get(context.TODO(), saObj.Name, metav1.GetOptions{})
	if err == nil {
		// Service account exists already. This mean the test is run again in cluster created previously
		// Secret should be created OR should be updated if exists
		tokenResponse, err := retryCreateFunctionWithResponse(token)
		if err != nil {
			return err
		}
		secret := secret(tokenResponse)
		err = createPrometheusSecretForExistingServiceAccount(k8sClient, secret)
		if err != nil {
			return err
		}
		return nil
	}
	// If ServiceAccount could not be retrieved but the error is not "not found", return it
	if !apierrs.IsNotFound(err) {
		return err
	}
	if err := retryCreateFunction(serviceAccount); err != nil {
		return err
	}
	tokenResponse, err := retryCreateFunctionWithResponse(token)
	if err != nil {
		return err
	}
	_, err = k8sClient.CoreV1().Secrets(namespace).Create(context.TODO(), secret(tokenResponse), metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func createPrometheusSecretForExistingServiceAccount(k8sClient kubernetes.Interface, secret *corev1.Secret) error {
	// Check if the secret already exists
	_, inErr := k8sClient.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if inErr == nil {
		// Secret already exists, update it
		_, inErr = k8sClient.CoreV1().Secrets(namespace).Update(context.TODO(), secret, metav1.UpdateOptions{})
		if inErr != nil {
			return fmt.Errorf("failed to update secret %s in namespace %s: %w", secretName, namespace, inErr)
		}
		return nil
	}
	if apierrs.IsNotFound(inErr) {
		// Secret doesn't exist, create it
		_, inErr = k8sClient.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
		if inErr != nil {
			return fmt.Errorf("failed to create secret %s in namespace %s: %w", secretName, namespace, inErr)
		}
		return nil
	}
	// Error occurred while trying to get the secret
	return inErr
}

// configureRBACForMetrics creates a ClusterRole and ClusterRoleBinding
// to allow the monitoringServiceAccount (used by Prometheus) to scrape metrics.
func (pc *Controller) configureRBACForMetrics(testClusterClientSet kubernetes.Interface) error {
	klog.V(2).Info("Exposing kube-apiserver metrics in the cluster")
	createClusterRole := func() error {
		_, err := testClusterClientSet.RbacV1().ClusterRoles().Create(context.TODO(), &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "metrics-viewer"},
			Rules: []rbacv1.PolicyRule{
				{Verbs: []string{"get"}, NonResourceURLs: []string{"/metrics"}},
				{APIGroups: []string{""}, Verbs: []string{"get"}, Resources: []string{"nodes/metrics"}},
			},
		}, metav1.CreateOptions{})
		return err
	}
	createClusterRoleBinding := func() error {
		_, err := testClusterClientSet.RbacV1().ClusterRoleBindings().Create(context.TODO(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "metrics-viewer-binding"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "metrics-viewer"},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Name: monitoringServiceAccount, Namespace: corev1.NamespaceDefault},
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
		if util.IsControlPlaneNode(&node) {
			numMasters++
			g.Go(func() error {
				f, err := manifestsFS.Open(nodeExporterPod)
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
	return wait.PollImmediate(
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

func retryCreateFunctionWithResponse(f func() (string, error)) (string, error) {
	var result string
	err := client.RetryWithExponentialBackOff(
		client.RetryFunction(func() error {
			var err error
			result, err = f()
			return err
		}, client.Allow(apierrs.IsAlreadyExists)))

	if err != nil {
		return "", err
	}
	return result, nil
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

func getMasterIps(clusterConfig config.ClusterConfig, usePublicIPs bool) ([]string, error) {
	if usePublicIPs {
		if len(clusterConfig.MasterIPs) == 0 {
			return nil, fmt.Errorf("requested to use public IPs, however no publics IPs are provided")
		}
		return clusterConfig.MasterIPs, nil
	}
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
