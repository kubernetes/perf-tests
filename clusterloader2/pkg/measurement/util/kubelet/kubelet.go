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
// and returns the resource usage of all containerNames for the past
// cpuInterval.
// The acceptable range of the interval is 2s~120s. Be warned that as the
// interval (and #containers) increases, the size of kubelet's response
// could be significant. E.g., the 60s interval stats for ~20 containers is
// ~1.5MB. Don't hammer the node with frequent, heavy requests.
//
// cadvisor records cumulative cpu usage in nanoseconds, so we need to have two
// stats points to compute the cpu usage over the interval. Assuming cadvisor
// polls every second, we'd need to get N stats points for N-second interval.
// Note that this is an approximation and may not be accurate, hence we also
// write the actual interval used for calculation (based on the timestamps of
// the stats points in ContainerResourceUsage.CPUInterval.
//
// containerNames is a function returning a collection of container names in which
// user is interested in.
func GetOneTimeResourceUsageOnNode(
	c clientset.Interface,
	nodeName string,
	cpuInterval time.Duration,
	containerNames func() []string,
) (util.ResourceUsagePerContainer, error) {
	const (
		// cadvisor records stats about every second.
		cadvisorStatsPollingIntervalInSeconds float64 = 1.0
		// cadvisor caches up to 2 minutes of stats (configured by kubelet).
		maxNumStatsToRequest int = 120
	)

	numStats := int(float64(cpuInterval.Seconds()) / cadvisorStatsPollingIntervalInSeconds)
	if numStats < 2 || numStats > maxNumStatsToRequest {
		return nil, fmt.Errorf("numStats (%v) needs to be > 1 and < %d", numStats, maxNumStatsToRequest)
	}
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
	observedContainers := []string{}
	for _, pod := range summary.Pods {
		for _, container := range pod.Containers {
			isInteresting := false
			for _, interestingContainerName := range containers {
				if container.Name == interestingContainerName {
					isInteresting = true
					observedContainers = append(observedContainers, container.Name)
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
