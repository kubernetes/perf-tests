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
	"fmt"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"os"
	"path/filepath"
)

const (
	namespace = "monitoring"
)

// SetUpPrometheusStack sets up prometheus stack in the cluster.
// This method is idempotent, if the prometheus stack is already set up applying the manifests
// again will be no-op.
func SetUpPrometheusStack(
	framework *framework.Framework, clusterLoaderConfig *config.ClusterLoaderConfig) error {

	k8sClient := framework.GetClientSets().GetClient()
	klog.Info("Setting up prometheus stack")
	if err := client.CreateNamespace(k8sClient, namespace); err != nil {
		return err
	}
	if err := applyManifests(framework, clusterLoaderConfig); err != nil {
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
