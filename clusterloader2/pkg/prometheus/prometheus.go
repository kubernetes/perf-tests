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
	"fmt"
	"os"
	"path/filepath"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
)

const (
	namespace                    = "monitoring"
	coreManifests                = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/*.yaml"
	defaultServiceMonitors       = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/default/*.yaml"
	kubemarkServiceMonitors      = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/kubemark/*.yaml"
	checkPrometheusReadyInterval = 30 * time.Second
	checkPrometheusReadyTimeout  = 15 * time.Minute
	numK8sClients                = 1
)

// PrometheusController is a util for managing (setting up / tearing down) the prometheus stack in
// the cluster.
type PrometheusController struct {
	clusterLoaderConfig *config.ClusterLoaderConfig
	// isKubemark determines whether prometheus stack is being set up in kubemark cluster or not.
	isKubemark bool
	// framework associated with the cluster where the prometheus stack should be set up.
	// For kubemark it's the root cluster, otherwise it's the main (and only) cluster.
	framework *framework.Framework
	// templateMapping is a mapping defining placeholders used in manifest templates.
	templateMapping map[string]interface{}
}

// NewPrometheusController creates a new instance of PrometheusController for the given config.
func NewPrometheusController(clusterLoaderConfig *config.ClusterLoaderConfig) (pc *PrometheusController, err error) {
	pc = &PrometheusController{
		clusterLoaderConfig: clusterLoaderConfig,
		isKubemark:          clusterLoaderConfig.ClusterConfig.Provider == "kubemark",
	}

	kubeConfigPath := clusterLoaderConfig.ClusterConfig.KubeConfigPath
	if pc.isKubemark {
		// For kubemark we will be setting up Prometheus stack in the root cluster.
		kubeConfigPath = clusterLoaderConfig.ClusterConfig.KubemarkRootKubeConfigPath
	}
	if pc.framework, err = framework.NewFramework(kubeConfigPath, numK8sClients); err != nil {
		return nil, err
	}

	mapping, errList := config.GetMapping(clusterLoaderConfig)
	if errList != nil {
		return nil, errList
	}
	if pc.isKubemark {
		mapping["KubemarkMasterIp"] = clusterLoaderConfig.ClusterConfig.MasterIP
	}
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
	if err := pc.applyManifests(coreManifests); err != nil {
		return err
	}
	if pc.isKubemark {
		if err := pc.exposeKubemarkApiServerMetrics(); err != nil {
			return err
		}
		if err := pc.applyManifests(kubemarkServiceMonitors); err != nil {
			return err
		}
	} else {
		if err := pc.applyManifests(defaultServiceMonitors); err != nil {
			return err
		}
	}
	if err := pc.waitForPrometheusToBeHealthy(); err != nil {
		return err
	}
	klog.Info("Prometheus stack set up successfully")
	return nil
}

// TearDownPrometheusStack tears down prometheus stack, releasing all prometheus resources.
func (pc *PrometheusController) TearDownPrometheusStack() error {
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

func (pc *PrometheusController) applyManifests(manifestGlob string) error {
	// TODO(mm4tt): Consider using the out-of-the-box "kubectl create -f".
	manifestGlob = os.ExpandEnv(manifestGlob)
	templateProvider := config.NewTemplateProvider(filepath.Dir(manifestGlob))
	manifests, err := filepath.Glob(manifestGlob)
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		klog.Infof("Applying %s\n", manifest)
		obj, err := templateProvider.TemplateToObject(filepath.Base(manifest), pc.templateMapping)
		if err != nil {
			return err
		}
		if obj.IsList() {
			objList, err := obj.ToList()
			if err != nil {
				return err
			}
			for _, item := range objList.Items {
				if err := pc.framework.CreateObject(item.GetNamespace(), item.GetName(), &item); err != nil {
					return fmt.Errorf("error while applying (%s): %v", manifest, err)
				}
			}
		} else {
			if err := pc.framework.CreateObject(obj.GetNamespace(), obj.GetName(), obj); err != nil {
				return fmt.Errorf("error while applying (%s): %v", manifest, err)
			}
		}
	}
	return nil
}

// exposeKubemarkApiServerMetrics configures anonymous access to the apiserver metrics in the
// kubemark cluster.
func (pc *PrometheusController) exposeKubemarkApiServerMetrics() error {
	klog.Info("Exposing kube-apiserver metrics in kubemark cluster")
	// This has to be done in the kubemark cluster, thus we need to create a new client.
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

func (pc *PrometheusController) waitForPrometheusToBeHealthy() error {
	klog.Info("Waiting for Prometheus stack to become healthy...")
	return wait.Poll(
		checkPrometheusReadyInterval,
		checkPrometheusReadyTimeout,
		pc.isPrometheusReady)
}

func (pc *PrometheusController) isPrometheusReady() (bool, error) {
	raw, err := pc.framework.GetClientSets().GetClient().CoreV1().
		Services(namespace).
		ProxyGet("http", "prometheus-k8s", "9090", "api/v1/targets", nil /*params*/).
		DoRaw()
	if err != nil {
		// This might happen if prometheus server is temporary down, log error but don't return it.
		klog.Warningf("error while calling prometheus api: %v", err)
		return false, nil
	}

	var response targetsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		// This shouldn't happen, return error.
		return false, err
	}

	// There should be at least as many targets as number of nodes (e.g. there is a kube-proxy
	// instance on each node). This is a safeguard from a race condition where the prometheus
	// server is started before targets are registered.
	expectedTargets := pc.clusterLoaderConfig.ClusterConfig.Nodes
	// TODO(mm4tt): Start monitoring kube-proxy in kubemark and get rid of this if.
	if pc.isKubemark {
		expectedTargets = 3 // kube-apiserver, prometheus, grafana
	}
	if len(response.Data.ActiveTargets) < expectedTargets {
		klog.Infof("Not enough active targets (%d), expected at least (%d), waiting for more to become active...",
			len(response.Data.ActiveTargets), expectedTargets)
		return false, nil
	}

	nReady := 0
	for _, t := range response.Data.ActiveTargets {
		if t.Health == "up" {
			nReady++
		}
	}
	if nReady < len(response.Data.ActiveTargets) {
		klog.Infof("%d/%d targets are ready", nReady, len(response.Data.ActiveTargets))
		return false, nil
	}
	klog.Infof("All %d targets are ready", len(response.Data.ActiveTargets))
	return true, nil
}

type targetsResponse struct {
	Data targetsData `json:"data""`
}

type targetsData struct {
	ActiveTargets []target `json:"activeTargets"`
}

type target struct {
	Labels map[string]string `json:"labels"`
	Health string            `json:"health"`
}

func retryCreateFunction(f func() error) error {
	return client.RetryWithExponentialBackOff(client.RetryFunction(f, apierrs.IsAlreadyExists))
}
