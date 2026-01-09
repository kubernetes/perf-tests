/*
Copyright 2025 The Kubernetes Authors.

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

package dra

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/dependency"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	draDependencyName      = "DRATestDriver"
	draNamespace           = "dra-example-driver"
	draManifests           = "dra-example-driver"
	defaultWorkerNodeCount = "100"
	draDaemonsetName       = "dra-example-driver-kubeletplugin"
	checkDRAReadyInterval  = 30 * time.Second
	defaultDRATimeout      = 10 * time.Minute
)

//go:embed manifests/**/*.yaml
var manifestsFS embed.FS

func init() {
	if err := dependency.Register(draDependencyName, createDRADependency); err != nil {
		klog.Fatalf("Cannot register %s: %v", draDependencyName, err)
	}
}

func createDRADependency() dependency.Dependency {
	return &draDependency{}
}

type draDependency struct{}

func (d *draDependency) Setup(config *dependency.Config) error {
	klog.V(2).Infof("%s: Installing DRA example driver", d)

	namespace, err := getNamespace(config)
	if err != nil {
		return err
	}

	if err := client.CreateNamespace(config.ClusterFramework.GetClientSets().GetClient(), namespace); err != nil {
		return fmt.Errorf("namespace %s creation error: %v", namespace, err)
	}

	manifests, err := getManifests(config)
	if err != nil {
		return err
	}

	daemonsetName, err := getDaemonset(config)
	if err != nil {
		return err
	}

	mapping := map[string]interface{}{
		"Namespace":       namespace,
		"WorkerNodeCount": getWorkerCount(config),
	}

	if extendedResourceName, ok := config.Params["ExtendedResourceName"]; ok {
		mapping["ExtendedResourceName"] = extendedResourceName
	}
	if err := config.ClusterFramework.ApplyTemplatedManifests(
		manifestsFS,
		manifests,
		mapping,
		client.Retry(client.IsRetryableAPIError),
	); err != nil {
		return fmt.Errorf("applying DRA manifests error: %v", err)
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultDRATimeout)
	if err != nil {
		return err
	}
	klog.V(2).Infof("%s: checking if DRA driver %s is healthy", d, daemonsetName)
	if err := d.waitForDRADriverToBeHealthy(config, timeout, daemonsetName, namespace); err != nil {
		return err
	}

	klog.V(2).Infof("%s: DRA example driver installed successfully", d)
	return nil
}

func (d *draDependency) Teardown(config *dependency.Config) error {
	klog.V(2).Infof("%s: Tearing down DRA example driver", d)

	namespace, err := getNamespace(config)
	if err != nil {
		return err
	}

	// Delete namespace (this will delete all resources in it)
	if err := client.DeleteNamespace(config.ClusterFramework.GetClientSets().GetClient(), namespace); err != nil {
		return fmt.Errorf("deleting %s namespace error: %v", namespace, err)
	}

	if err := client.WaitForDeleteNamespace(config.ClusterFramework.GetClientSets().GetClient(), namespace, client.DefaultNamespaceDeletionTimeout); err != nil {
		return err
	}

	klog.V(2).Infof("%s: DRA example driver uninstalled successfully", d)
	return nil
}

func (d *draDependency) waitForDRADriverToBeHealthy(config *dependency.Config, timeout time.Duration, daemonsetName string, namespace string) error {
	if err := wait.PollImmediate(
		checkDRAReadyInterval,
		timeout,
		func() (done bool, err error) {
			return d.isDRADriverReady(config, daemonsetName, namespace)
		}); err != nil {
		return err
	}
	if err := wait.PollImmediate(
		checkDRAReadyInterval,
		timeout,
		func() (done bool, err error) {
			return isResourceSlicesPublished(config, namespace)
		}); err != nil {
		return err
	}
	return nil
}

func (d *draDependency) isDRADriverReady(config *dependency.Config, daemonsetName string, namespace string) (done bool, err error) {
	ds, err := config.ClusterFramework.GetClientSets().
		GetClient().
		AppsV1().
		DaemonSets(namespace).
		Get(context.Background(), daemonsetName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get %s: %v", daemonsetName, err)
	}
	ready := ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	if !ready {
		klog.V(2).Infof("%s is not ready, "+
			"DesiredNumberScheduled: %d, NumberReady: %d", daemonsetName, ds.Status.DesiredNumberScheduled, ds.Status.NumberReady)
	}
	return ready, nil
}

func isResourceSlicesPublished(config *dependency.Config, namespace string) (bool, error) {
	// Get a list of all nodes
	// nodes, err := getReadyNodesCount(config)
	// if err != nil {
	// 	return false, fmt.Errorf("failed to list nodes: %v", err)
	// }

	driverPluginPods, err := getDriverPluginPods(config, namespace, draDaemonsetName)
	if err != nil {
		return false, fmt.Errorf("failed to list driverPluginPods: %v", err)
	}

	workerCount := driverPluginPods

	resourceSlices, err := config.ClusterFramework.GetClientSets().GetClient().ResourceV1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list resourceslices: %v", err)
	}
	if len(resourceSlices.Items) != workerCount {
		klog.V(2).Infof("waiting for resourceslices to be available, "+
			"DesiredResourceSliceCount: %d, NumberResourceSlicesAvailable: %d", workerCount, len(resourceSlices.Items))
		return false, nil
	}
	return true, nil
}

func getDriverPluginPods(config *dependency.Config, namespace string, namePrefix string) (int, error) {
	pods, err := config.ClusterFramework.GetClientSets().GetClient().CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}

	runningPods := 0
	for _, pod := range pods.Items {
		if !strings.HasPrefix(pod.Name, namePrefix) {
			continue
		}

		if pod.Status.Phase == corev1.PodRunning {
			runningPods++
		}
	}

	return runningPods, nil
}

func getWorkerCount(config *dependency.Config) interface{} {
	workerCount, ok := config.Params["WorkerNodeCount"]
	if !ok {
		workerCount = defaultWorkerNodeCount
	}
	return workerCount
}

func getNamespace(config *dependency.Config) (string, error) {
	namespace, ok := config.Params["Namespace"]
	if !ok {
		namespace = draNamespace
	}
	namespaceString, ok := namespace.(string)

	if !ok {
		return "", fmt.Errorf("namespace parameter is not a string: %v", namespace)
	}
	return namespaceString, nil
}

func getManifests(config *dependency.Config) (string, error) {
	manifests, ok := config.Params["Manifests"]
	if !ok {
		manifests = draManifests
	}
	manifestsString, ok := manifests.(string)
	if !ok {
		return "", fmt.Errorf("manifests parameter is not a string: %v", manifests)
	}
	return "manifests/" + manifestsString + "/*.yaml", nil
}

func getDaemonset(config *dependency.Config) (string, error) {
	daemonsetName, ok := config.Params["DaemonsetName"]
	if !ok {
		daemonsetName = draDaemonsetName
	}
	daemonsetNameString, ok := daemonsetName.(string)
	if !ok {
		return "", fmt.Errorf("DaemonsetName parameter is not a string: %v", daemonsetName)
	}
	return daemonsetNameString, nil
}

// String returns string representation of this dependency.
func (d *draDependency) String() string {
	return draDependencyName
}
