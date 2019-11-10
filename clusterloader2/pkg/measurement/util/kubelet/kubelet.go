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

package kubelet

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"k8s.io/kubernetes/pkg/master/ports"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

const (
	singleCallTimeout = 5 * time.Minute
)

// GetOneTimeResourceUsageOnNode queries the node's /stats/summary endpoint
// and returns the resource usage of all containerNames returned by the containerNames function.
func GetOneTimeResourceUsageOnNode(c clientset.Interface, nodeName string, containerNames func() []string) (util.ResourceUsagePerContainer, error) {
	// Get information of all containers on the node.
	summary, err := getStatsSummary(c, nodeName)
	if err != nil {
		return nil, err
	}

	f := func(name string, newStats *stats.ContainerStats) *util.ContainerResourceUsage {
		if newStats == nil || newStats.CPU == nil || newStats.Memory == nil {
			return nil
		}
		return &util.ContainerResourceUsage{
			Name:                    name,
			Timestamp:               newStats.StartTime.Time,
			CPUUsageInCores:         float64(removeUint64Ptr(newStats.CPU.UsageNanoCores)) / 1000000000,
			MemoryUsageInBytes:      removeUint64Ptr(newStats.Memory.UsageBytes),
			MemoryWorkingSetInBytes: removeUint64Ptr(newStats.Memory.WorkingSetBytes),
			MemoryRSSInBytes:        removeUint64Ptr(newStats.Memory.RSSBytes),
			CPUInterval:             0,
		}
	}
	// Process container infos that are relevant to us.
	containers := containerNames()
	usageMap := make(util.ResourceUsagePerContainer, len(containers))
	for _, pod := range summary.Pods {
		for _, container := range pod.Containers {
			isInteresting := false
			for _, interestingContainerName := range containers {
				if container.Name == interestingContainerName {
					isInteresting = true
					break
				}
			}
			if !isInteresting {
				continue
			}
			if usage := f(pod.PodRef.Name+"/"+container.Name, &container); usage != nil {
				usageMap[pod.PodRef.Name+"/"+container.Name] = usage
			}
		}
	}
	return usageMap, nil
}

// getStatsSummary contacts kubelet for the container information.
func getStatsSummary(c clientset.Interface, nodeName string) (*stats.Summary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), singleCallTimeout)
	defer cancel()

	data, err := c.CoreV1().RESTClient().Get().
		Context(ctx).
		Resource("nodes").
		SubResource("proxy").
		Name(fmt.Sprintf("%v:%v", nodeName, ports.KubeletPort)).
		Suffix("stats/summary").
		Do().Raw()

	if err != nil {
		return nil, err
	}

	summary := stats.Summary{}
	err = json.Unmarshal(data, &summary)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func removeUint64Ptr(ptr *uint64) uint64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}
