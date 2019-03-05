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

	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
)

const (
	namespace                    = "monitoring"
	checkPrometheusReadyInterval = 30 * time.Second
	checkPrometheusReadyTimeout  = 15 * time.Minute
)

// SetUpPrometheusStack sets up prometheus stack in the cluster.
// This method is idempotent, if the prometheus stack is already set up applying the manifests
// again will be no-op.
func SetUpPrometheusStack(
	framework *framework.Framework, clusterLoaderConfig *config.ClusterLoaderConfig) error {

	k8sClient := framework.GetClientSets().GetClient()
	nodeCount := clusterLoaderConfig.ClusterConfig.Nodes

	klog.Info("Setting up prometheus stack")
	if err := client.CreateNamespace(k8sClient, namespace); err != nil {
		return err
	}
	if err := applyManifests(framework, clusterLoaderConfig); err != nil {
		return err
	}
	if err := waitForPrometheusToBeHealthy(k8sClient, nodeCount); err != nil {
		return err
	}
	klog.Info("Prometheus stack set up successfully")
	return nil
}

// TearDownPrometheusStack tears down prometheus stack, releasing all prometheus resources.
func TearDownPrometheusStack(framework *framework.Framework) error {
	klog.Info("Tearing down prometheus stack")
	k8sClient := framework.GetClientSets().GetClient()
	if err := client.DeleteNamespace(k8sClient, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(k8sClient, namespace); err != nil {
		return err
	}
	return nil
}

func applyManifests(
	framework *framework.Framework, clusterLoaderConfig *config.ClusterLoaderConfig) error {
	// TODO(mm4tt): Consider using the out-of-the-box "kubectl create -f".
	manifestGlob := os.ExpandEnv(
		"$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/*.yaml")
	templateProvider := config.NewTemplateProvider(filepath.Dir(manifestGlob))
	mapping, errList := config.GetMapping(clusterLoaderConfig)
	if errList != nil && !errList.IsEmpty() {
		return errList
	}
	manifests, err := filepath.Glob(manifestGlob)
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		klog.Infof("Applying %s\n", manifest)
		obj, err := templateProvider.TemplateToObject(filepath.Base(manifest), mapping)
		if err != nil {
			return err
		}
		if obj.IsList() {
			objList, err := obj.ToList()
			if err != nil {
				return err
			}
			for _, item := range objList.Items {
				if err := framework.CreateObject(item.GetNamespace(), item.GetName(), &item); err != nil {
					return fmt.Errorf("error while applying (%s): %v", manifest, err)
				}
			}
		} else {
			if err := framework.CreateObject(obj.GetNamespace(), obj.GetName(), obj); err != nil {
				return fmt.Errorf("error while applying (%s): %v", manifest, err)
			}
		}
	}
	return nil
}

func waitForPrometheusToBeHealthy(client clientset.Interface, nodeCount int) error {
	klog.Info("Waiting for Prometheus stack to become healthy...")
	return wait.Poll(
		checkPrometheusReadyInterval,
		checkPrometheusReadyTimeout,
		func() (bool, error) { return isPrometheusReady(client, nodeCount) })
}

func isPrometheusReady(client clientset.Interface, nodeCount int) (bool, error) {
	raw, err := client.CoreV1().
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

	if len(response.Data.ActiveTargets) < nodeCount {
		// There should be at least as many targets as number of nodes (e.g. there is a kube-proxy
		// instance on each node). This is a safeguard from a race condition where the prometheus
		// server is started before targets are registered.
		klog.Infof("Less active targets (%d) than nodes (%d), waiting for more to become active...",
			len(response.Data.ActiveTargets), nodeCount)
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
