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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/dependency"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	draDependencyName = "DRATestDriver"
	//TODO: this needs to be converted into a parameter. Will will not need this until parititionable devices test
	draNamespace           = "dra-example-driver"
	defaultWorkerNodeCount = "100"
	draDaemonsetName       = "dra-example-driver-kubeletplugin"
	checkDRAReadyInterval  = 30 * time.Second
	defaultDRATimeout      = 10 * time.Minute
)

//go:embed manifests/*.yaml
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
	if err := client.CreateNamespace(config.ClusterFramework.GetClientSets().GetClient(), draNamespace); err != nil {
		return fmt.Errorf("namespace %s creation error: %v", draNamespace, err)
	}

	namespace, ok := config.Params["Namespace"]
	if !ok {
		namespace = draNamespace
	}

	mapping := map[string]interface{}{
		"Namespace":       namespace,
		"WorkerNodeCount": getWorkerCount(config),
	}
	if err := config.ClusterFramework.ApplyTemplatedManifests(
		manifestsFS,
		"manifests/*.yaml",
		mapping,
		client.Retry(client.IsRetryableAPIError),
	); err != nil {
		return fmt.Errorf("applying DRA manifests error: %v", err)
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultDRATimeout)
	if err != nil {
		return err
	}
	klog.V(2).Infof("%s: checking if DRA driver %s is healthy", d, draDaemonsetName)
	if err := d.waitForDRADriverToBeHealthy(config, timeout); err != nil {
		return err
	}

	klog.V(2).Infof("%s: DRA example driver installed successfully", d)
	return nil
}

func (d *draDependency) Teardown(config *dependency.Config) error {
	klog.V(2).Infof("%s: Tearing down DRA example driver", d)

	// Delete namespace (this will delete all resources in it)
	if err := client.DeleteNamespace(config.ClusterFramework.GetClientSets().GetClient(), draNamespace); err != nil {
		return fmt.Errorf("deleting %s namespace error: %v", draNamespace, err)
	}

	if err := client.WaitForDeleteNamespace(config.ClusterFramework.GetClientSets().GetClient(), draNamespace, client.DefaultNamespaceDeletionTimeout); err != nil {
		return err
	}

	klog.V(2).Infof("%s: DRA example driver uninstalled successfully", d)
	return nil
}

func (d *draDependency) waitForDRADriverToBeHealthy(config *dependency.Config, timeout time.Duration) error {
	if err := wait.PollImmediate(
		checkDRAReadyInterval,
		timeout,
		func() (done bool, err error) {
			return d.isDRADriverReady(config)
		}); err != nil {
		return err
	}
	if err := wait.PollImmediate(
		checkDRAReadyInterval,
		timeout,
		func() (done bool, err error) {
			return isResourceSlicesPublished(config)
		}); err != nil {
		return err
	}
	return nil
}

func (d *draDependency) isDRADriverReady(config *dependency.Config) (done bool, err error) {
	ds, err := config.ClusterFramework.GetClientSets().
		GetClient().
		AppsV1().
		DaemonSets(draNamespace).
		Get(context.Background(), draDaemonsetName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get %s: %v", draDaemonsetName, err)
	}
	ready := ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	if !ready {
		klog.V(2).Infof("%s is not ready, "+
			"DesiredNumberScheduled: %d, NumberReady: %d", draDaemonsetName, ds.Status.DesiredNumberScheduled, ds.Status.NumberReady)
	}
	return ready, nil
}

func isResourceSlicesPublished(config *dependency.Config) (bool, error) {
	workerCount := int(getWorkerCount(config).(float64))

	resourceSlices, err := config.ClusterFramework.GetClientSets().GetClient().ResourceV1beta1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
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

func getWorkerCount(config *dependency.Config) interface{} {
	workerCount, ok := config.Params["WorkerNodeCount"]
	if !ok {
		workerCount = defaultWorkerNodeCount
	}
	return workerCount
}

// String returns string representation of this dependency.
func (d *draDependency) String() string {
	return draDependencyName
}
