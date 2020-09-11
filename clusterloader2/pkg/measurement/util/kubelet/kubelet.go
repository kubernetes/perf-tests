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
	"net"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"k8s.io/kubernetes/pkg/master/ports"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

const (
	singleCallTimeout = 5 * time.Minute
)

type ContainerFilter func(pod stats.PodReference, name string) bool

// ContainerIDFilter matches containers by name.
func ContainerIDFilter(ids func() []string) ContainerFilter {
	return func(pod stats.PodReference, name string) bool {
		for _, id := range ids() {
			if id == name {
				return true
			}
		}
		return false
	}
}

// AllContainersFilter matches any containers.
func AllContainersFilter() ContainerFilter {
	return func(pod stats.PodReference, name string) bool {
		return true
	}
}

// GetOneTimeResourceUsageOnNode queries the node's /stats/summary endpoint
// and returns the resource usage of all containerNames returned by the containerNames function.
func GetOneTimeResourceUsageOnNode(c clientset.Interface, nodeName string, port int, containerNames func() []string) (util.ResourceUsagePerContainer, error) {
	return GetOneTimeResourceUsageOnNodeFilter(APIStatsGatherer(c), nodeName, port, ContainerIDFilter(containerNames))
}

type StatsGatherer func(nodeName string, port int) (*stats.Summary, error)

// GetOneTimeResourceUsageOnNodeFilter queries the node's /stats/summary endpoint
// and returns the resource usage of all containers that matched filter.
func GetOneTimeResourceUsageOnNodeFilter(gatherer StatsGatherer, nodeName string, port int, filter ContainerFilter) (util.ResourceUsagePerContainer, error) {
	// Get information of all containers on the node.
	summary, err := gatherer(nodeName, port)
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
	usageMap := make(util.ResourceUsagePerContainer)
	for _, pod := range summary.Pods {
		for _, container := range pod.Containers {
			if !filter(pod.PodRef, container.Name) {
				continue
			}
			if usage := f(pod.PodRef.Name+"/"+container.Name, &container); usage != nil {
				usageMap[pod.PodRef.Name+"/"+container.Name] = usage
			}
		}
	}
	return usageMap, nil
}

// APIStatsGatherer uses nodes/proxy to collect stats.
func APIStatsGatherer(c clientset.Interface) StatsGatherer {
	return func(nodeName string, port int) (summary *stats.Summary, err error) {
		return getStatsSummary(c, nodeName, port)
	}
}

// getStatsSummary contacts kubelet for the container information.
func getStatsSummary(c clientset.Interface, nodeName string, port int) (*stats.Summary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), singleCallTimeout)
	defer cancel()

	data, err := c.CoreV1().RESTClient().Get().
		Resource("nodes").
		SubResource("proxy").
		Name(fmt.Sprintf("%v:%v", nodeName, port)).
		Suffix("stats/summary").
		Do(ctx).Raw()

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

// SSHStatsGatherer grabs pod resource usage summary using ssh.
func SSHStatsGatherer(p provider.Provider, sshAddress string) StatsGatherer {
	return func(nodeName string, port int) (*stats.Summary, error) {
		cmd := fmt.Sprintf("curl http://localhost:%v/stats/summary", ports.KubeletReadOnlyPort)
		stdout, stderr, code, err := p.RunSSHCommand(cmd, net.JoinHostPort(sshAddress, "22"))
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, fmt.Errorf("failed to run %q on %q, stderr: %v", cmd, sshAddress, stderr)
		}
		summary := stats.Summary{}
		err = json.Unmarshal([]byte(stdout), &summary)
		if err != nil {
			return nil, err
		}
		return &summary, nil
	}
}
