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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/perf-tests/clusterloader2/pkg/dependency"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	kwokDRADependencyName    = "DRAKWOKDriver"
	kwokNamespace            = "kwok-system"
	kwokControllerDeployment = "kwok-controller"
	checkKwokReadyInterval   = 15 * time.Second
	defaultKwokReadyTimeout  = 5 * time.Minute
)

//go:embed manifests/*.yaml manifests/crds/*.yaml
var manifestsFS embed.FS

func init() {
	if err := dependency.Register(kwokDRADependencyName, createKWOKDRADependency); err != nil {
		klog.Fatalf("Cannot register %s: %v", kwokDRADependencyName, err)
	}
}

func createKWOKDRADependency() dependency.Dependency {
	return &kwokDRADependency{}
}

type kwokDRADependency struct{}

// Setup installs the KWOK controller and accompanying fake DRA resources.
func (d *kwokDRADependency) Setup(cfg *dependency.Config) error {
	klog.V(2).Infof("%s: installing KWOK controller", d)

	if err := client.CreateNamespace(cfg.ClusterFramework.GetClientSets().GetClient(), kwokNamespace); err != nil {
		return fmt.Errorf("namespace %s creation error: %v", kwokNamespace, err)
	}

	coreManifests := []string{
		"manifests/service_account.yaml",
		"manifests/role.yaml",
		"manifests/role_binding.yaml",
		"manifests/kwok.yaml",
		"manifests/service.yaml",
		"manifests/deployment.yaml",
		"manifests/flow_schema.yaml",
		"manifests/device-class.yaml",
	}

	crdManifests := []string{
		"manifests/crds/kwok.x-k8s.io_attaches.yaml",
		"manifests/crds/kwok.x-k8s.io_clusterattaches.yaml",
		"manifests/crds/kwok.x-k8s.io_clusterexecs.yaml",
		"manifests/crds/kwok.x-k8s.io_clusterlogs.yaml",
		"manifests/crds/kwok.x-k8s.io_clusterportforwards.yaml",
		"manifests/crds/kwok.x-k8s.io_clusterresourceusages.yaml",
		"manifests/crds/kwok.x-k8s.io_execs.yaml",
		"manifests/crds/kwok.x-k8s.io_logs.yaml",
		"manifests/crds/kwok.x-k8s.io_metrics.yaml",
		"manifests/crds/kwok.x-k8s.io_portforwards.yaml",
		"manifests/crds/kwok.x-k8s.io_resourceusages.yaml",
		"manifests/crds/kwok.x-k8s.io_stages.yaml",
	}

	for _, manifest := range crdManifests {
		klog.V(2).Infof("%s: applying CRD %s", d, manifest)
		if err := cfg.ClusterFramework.ApplyTemplatedManifests(
			manifestsFS,
			manifest,
			map[string]interface{}{},
			client.Retry(client.IsRetryableAPIError),
		); err != nil {
			return fmt.Errorf("applying CRD %s error: %v", manifest, err)
		}
	}

	for _, manifest := range coreManifests {
		klog.V(2).Infof("%s: applying %s", d, manifest)
		if err := cfg.ClusterFramework.ApplyTemplatedManifests(
			manifestsFS,
			manifest,
			map[string]interface{}{},
			client.Retry(client.IsRetryableAPIError),
		); err != nil {
			return fmt.Errorf("applying %s error: %v", manifest, err)
		}
		klog.V(4).Infof("%s: successfully applied %s", d, manifest)
	}

	timeout, err := dependencyWaitTimeout(cfg, defaultKwokReadyTimeout)
	if err != nil {
		return err
	}
	klog.V(2).Infof("%s: waiting up to %v for KWOK controller to be ready", d, timeout)
	if err := d.waitForKWOKToBeHealthy(cfg, timeout); err != nil {
		return err
	}

	// Now apply Stage manifests (KWOK CRDs are now registered).
	// Apply in order: node stages first (so nodes can become ready), then pod stages, then job stages
	stageManifests := []string{
		"manifests/stage_fast_node_initialize.yaml",
		"manifests/stage_fast_node.yaml",
		"manifests/stage_fast_pod_ready.yaml",
		"manifests/stage_fast_pod_delete.yaml",
		"manifests/job-completion-stages.yaml",
	}

	for _, stageManifest := range stageManifests {
		klog.V(2).Infof("%s: applying Stage manifest %s", d, stageManifest)
		if err := cfg.ClusterFramework.ApplyTemplatedManifests(
			manifestsFS,
			stageManifest,
			map[string]interface{}{},
			client.Retry(client.IsRetryableAPIError),
		); err != nil {
			return fmt.Errorf("applying Stage manifest %s error: %v", stageManifest, err)
		}
	}

	nodes, err := util.GetIntOrDefault(cfg.Params, "nodes", 2)
	if err != nil {
		return fmt.Errorf("invalid nodes param: %v", err)
	}
	gpusPerNode, err := util.GetIntOrDefault(cfg.Params, "gpusPerNode", 8)
	if err != nil {
		return fmt.Errorf("invalid gpusPerNode param: %v", err)
	}

	if err := d.createFakeClusterObjects(cfg, nodes, gpusPerNode); err != nil {
		return fmt.Errorf("creating fake cluster objects: %v", err)
	}

	klog.V(2).Infof("%s: waiting for %d nodes to be ready", d, nodes)
	if err := d.waitForNodesToBeReady(cfg, nodes, timeout); err != nil {
		return fmt.Errorf("waiting for nodes to be ready: %v", err)
	}

	klog.V(2).Infof("%s: KWOK controller along with dra devices installed successfully", d)
	return nil
}

func (d *kwokDRADependency) Teardown(cfg *dependency.Config) error {
	klog.V(2).Infof("%s: tearing down KWOK controller and cluster resources", d)

	clientset := cfg.ClusterFramework.GetClientSets().GetClient()
	dynamicClient := cfg.ClusterFramework.GetDynamicClients().GetClient()

	nodeList, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: "type=kwok",
	})
	if err != nil {
		klog.Warningf("%s: failed to list KWOK nodes: %v", d, err)
	} else {
		for _, node := range nodeList.Items {
			if err := clientset.CoreV1().Nodes().Delete(context.Background(), node.Name, metav1.DeleteOptions{}); err != nil {
				if !errors.IsNotFound(err) {
					klog.Warningf("%s: failed to delete node %s: %v", d, node.Name, err)
				}
			} else {
				klog.V(3).Infof("%s: deleted KWOK node %s", d, node.Name)
			}
		}

		// Give KWOK Stages time to simulate pod cleanup after node deletion
		if len(nodeList.Items) > 0 {
			klog.V(3).Infof("%s: waiting for KWOK to simulate pod cleanup after node deletion", d)
			time.Sleep(5 * time.Second)
		}
	}

	stageGVR := schema.GroupVersionResource{Group: "kwok.x-k8s.io", Version: "v1alpha1", Resource: "stages"}
	stageNames := []string{"job-complete-short", "job-complete-long", "node-heartbeat-with-lease", "node-initialize", "pod-delete", "pod-ready"}
	for _, stageName := range stageNames {
		if err := dynamicClient.Resource(stageGVR).Delete(context.Background(), stageName, metav1.DeleteOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				klog.Warningf("%s: failed to delete Stage %s: %v", d, stageName, err)
			}
		} else {
			klog.V(3).Infof("%s: deleted Stage %s", d, stageName)
		}
	}

	resourceSliceGVR := schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1beta2", Resource: "resourceslices"}
	sliceList, err := dynamicClient.Resource(resourceSliceGVR).List(context.Background(), metav1.ListOptions{
		LabelSelector: "resource.k8s.io/driver=cl2-gpu.kwok.x-k8s.io",
	})
	if err != nil {
		klog.Warningf("%s: failed to list ResourceSlices: %v", d, err)
	} else {
		for _, slice := range sliceList.Items {
			sliceName := slice.GetName()
			if err := dynamicClient.Resource(resourceSliceGVR).Delete(context.Background(), sliceName, metav1.DeleteOptions{}); err != nil {
				if !errors.IsNotFound(err) {
					klog.Warningf("%s: failed to delete ResourceSlice %s: %v", d, sliceName, err)
				}
			} else {
				klog.V(3).Infof("%s: deleted ResourceSlice %s", d, sliceName)
			}
		}
	}

	deviceClassGVR := schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1beta2", Resource: "deviceclasses"}
	deviceClassName := "cl2-gpu.kwok.x-k8s.io"
	if err := dynamicClient.Resource(deviceClassGVR).Delete(context.Background(), deviceClassName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			klog.Warningf("%s: failed to delete DeviceClass %s: %v", d, deviceClassName, err)
		}
	} else {
		klog.V(3).Infof("%s: deleted DeviceClass %s", d, deviceClassName)
	}

	if err := client.DeleteNamespace(clientset, kwokNamespace); err != nil {
		return fmt.Errorf("deleting %s namespace error: %v", kwokNamespace, err)
	}
	if err := client.WaitForDeleteNamespace(clientset, kwokNamespace, client.DefaultNamespaceDeletionTimeout); err != nil {
		return err
	}

	klog.V(2).Infof("%s: KWOK controller and all cluster resources removed", d)
	return nil
}

func (d *kwokDRADependency) waitForKWOKToBeHealthy(cfg *dependency.Config, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), checkKwokReadyInterval, timeout, true, func(ctx context.Context) (bool, error) {
		return d.isKWOKReady(ctx, cfg)
	})
}

func (d *kwokDRADependency) isKWOKReady(ctx context.Context, cfg *dependency.Config) (bool, error) {
	deploy, err := cfg.ClusterFramework.GetClientSets().GetClient().AppsV1().Deployments(kwokNamespace).Get(ctx, kwokControllerDeployment, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).Infof("KWOK deployment not found yet, continuing to wait...")
			return false, nil // Not ready yet, but not an error - continue polling
		}
		return false, fmt.Errorf("failed to get KWOK deployment: %v", err)
	}
	ready := deploy.Status.ReadyReplicas == *deploy.Spec.Replicas && deploy.Status.ReadyReplicas > 0
	if !ready {
		klog.V(4).Infof("KWOK controller not ready: replicas %d/%d", deploy.Status.ReadyReplicas, *deploy.Spec.Replicas)
	}
	return ready, nil
}

func (d *kwokDRADependency) waitForNodesToBeReady(cfg *dependency.Config, expectedNodes int, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), checkKwokReadyInterval, timeout, true, func(ctx context.Context) (bool, error) {
		return d.areNodesReady(ctx, cfg, expectedNodes)
	})
}

func (d *kwokDRADependency) areNodesReady(ctx context.Context, cfg *dependency.Config, expectedNodes int) (bool, error) {
	clientset := cfg.ClusterFramework.GetClientSets().GetClient()

	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "type=kwok",
	})
	if err != nil {
		return false, fmt.Errorf("failed to list KWOK nodes: %v", err)
	}

	if len(nodeList.Items) != expectedNodes {
		klog.V(4).Infof("Expected %d KWOK nodes, found %d", expectedNodes, len(nodeList.Items))
		return false, nil
	}

	readyNodes := 0
	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}

	if readyNodes != expectedNodes {
		klog.V(4).Infof("Expected %d ready KWOK nodes, found %d ready", expectedNodes, readyNodes)
		return false, nil
	}

	klog.V(3).Infof("All %d KWOK nodes are ready", expectedNodes)
	return true, nil
}

// createFakeClusterObjects creates Node and matching ResourceSlice objects for KWOK.
func (d *kwokDRADependency) createFakeClusterObjects(cfg *dependency.Config, nodeCount, gpusPerNode int) error {
	clientset := cfg.ClusterFramework.GetClientSets().GetClient()

	for i := 0; i < nodeCount; i++ {
		nodeName := fmt.Sprintf("kwok-node-%d", i)

		// Prepare Node object
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					"beta.kubernetes.io/arch":       "amd64",
					"beta.kubernetes.io/os":         "linux",
					"kubernetes.io/arch":            "amd64",
					"kubernetes.io/os":              "linux",
					"kubernetes.io/role":            "agent",
					"node-role.kubernetes.io/agent": "",
					"type":                          "kwok",
					"kubernetes.io/hostname":        nodeName,
				},
				Annotations: map[string]string{
					"node.alpha.kubernetes.io/ttl": "0",
					"kwok.x-k8s.io/node":           "fake",
				},
			},
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{{
					Key:    "kwok.x-k8s.io/node",
					Value:  "fake",
					Effect: v1.TaintEffectNoSchedule,
				}},
			},
			Status: v1.NodeStatus{
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("32"),
					v1.ResourceMemory: resource.MustParse("256Gi"),
					v1.ResourcePods:   resource.MustParse("110"),
				},
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("32"),
					v1.ResourceMemory: resource.MustParse("256Gi"),
					v1.ResourcePods:   resource.MustParse("110"),
				},
				Conditions: []v1.NodeCondition{
					{
						Type:               v1.NodeReady,
						Status:             v1.ConditionTrue,
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
						Reason:             "KubeletReady",
						Message:            "kubelet is posting ready status. AppArmor enabled",
					},
					{
						Type:               v1.NodeMemoryPressure,
						Status:             v1.ConditionFalse,
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
						Reason:             "KubeletHasSufficientMemory",
						Message:            "kubelet has sufficient memory available",
					},
					{
						Type:               v1.NodeDiskPressure,
						Status:             v1.ConditionFalse,
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
						Reason:             "KubeletHasNoDiskPressure",
						Message:            "kubelet has no disk pressure",
					},
					{
						Type:               v1.NodePIDPressure,
						Status:             v1.ConditionFalse,
						LastHeartbeatTime:  metav1.Now(),
						LastTransitionTime: metav1.Now(),
						Reason:             "KubeletHasSufficientPID",
						Message:            "kubelet has sufficient PID available",
					},
				},
				Phase: v1.NodeRunning,
				NodeInfo: v1.NodeSystemInfo{
					MachineID:               "fake-machine-id",
					SystemUUID:              "fake-system-uuid",
					BootID:                  "fake-boot-id",
					KernelVersion:           "5.4.0-fake",
					OSImage:                 "Ubuntu 20.04.1 LTS",
					ContainerRuntimeVersion: "containerd://1.6.0-fake",
					KubeletVersion:          "v1.29.0-fake",
					KubeProxyVersion:        "v1.29.0-fake",
					OperatingSystem:         "linux",
					Architecture:            "amd64",
				},
			},
		}

		// Create or update Node
		if _, err := clientset.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{}); err != nil {
			if !errors.IsAlreadyExists(err) {
				return fmt.Errorf("creating node %s: %v", nodeName, err)
			}
		}

		// Build ResourceSlice unstructured (because typed client may not exist)
		sliceName := fmt.Sprintf("kwok-gpu-node-%d", i)
		gvr := schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1beta2", Resource: "resourceslices"}

		deviceList := make([]interface{}, 0, gpusPerNode)
		for g := 0; g < gpusPerNode; g++ {
			deviceList = append(deviceList, map[string]interface{}{
				"name": fmt.Sprintf("gpu%d", g),
				"attributes": map[string]interface{}{
					"name": map[string]interface{}{
						"string": fmt.Sprintf("gpu_%d", g),
					},
					"gpu_type": map[string]interface{}{
						"string": "kwok_gpu",
					},
					"memory": map[string]interface{}{
						"string": "8Gi",
					},
					"compute_capability": map[string]interface{}{
						"string": "7.5",
					},
				},
			})
		}

		sliceObj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "resource.k8s.io/v1beta2",
			"kind":       "ResourceSlice",
			"metadata": map[string]interface{}{
				"name": sliceName,
				"labels": map[string]interface{}{
					"resource.k8s.io/driver": "cl2-gpu.kwok.x-k8s.io",
				},
			},
			"spec": map[string]interface{}{
				"driver":   "cl2-gpu.kwok.x-k8s.io",
				"nodeName": nodeName,
				"pool": map[string]interface{}{
					"name":               fmt.Sprintf("kwok-gpu-pool-node-%d", i),
					"generation":         int64(1),
					"resourceSliceCount": int64(1),
				},
				"devices": deviceList,
			},
		}}

		dynamicClient := cfg.ClusterFramework.GetDynamicClients().GetClient()
		if _, err := dynamicClient.Resource(gvr).Create(context.Background(), sliceObj, metav1.CreateOptions{}); err != nil {
			if !errors.IsAlreadyExists(err) {
				return fmt.Errorf("creating resourceslice %s: %v", sliceName, err)
			}
		}
	}
	return nil
}

func dependencyWaitTimeout(cfg *dependency.Config, def time.Duration) (time.Duration, error) {
	// look for "timeout" param in dependency params map
	if cfg == nil || cfg.Params == nil {
		return def, nil
	}
	return dependencyTimeoutFromParams(cfg.Params, def)
}

func dependencyTimeoutFromParams(params map[string]interface{}, def time.Duration) (time.Duration, error) {
	// util.GetDurationOrDefault cannot be used here to avoid import cycle; mimic.
	if raw, ok := params["timeout"]; ok {
		switch v := raw.(type) {
		case time.Duration:
			return v, nil
		case string:
			d, err := time.ParseDuration(v)
			if err != nil {
				return 0, fmt.Errorf("parsing timeout param: %v", err)
			}
			return d, nil
		}
	}
	return def, nil
}

func (d *kwokDRADependency) String() string { return kwokDRADependencyName }
