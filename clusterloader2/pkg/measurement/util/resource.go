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

package util

import (
	"math"
	"sort"
	"time"
)

// ContainerResourceUsage represents resource usage by a single container.
type ContainerResourceUsage struct {
	Name                    string
	Timestamp               time.Time
	CPUUsageInCores         float64
	MemoryUsageInBytes      uint64
	MemoryWorkingSetInBytes uint64
	MemoryRSSInBytes        uint64
	// The interval used to calculate CPUUsageInCores.
	CPUInterval time.Duration
}

// ResourceUsagePerContainer is a map of ContainerResourceUsage for containers.
type ResourceUsagePerContainer map[string]*ContainerResourceUsage

// UsageDataPerContainer contains resource usage data series.
type UsageDataPerContainer struct {
	CpuData        []float64
	MemUseData     []uint64
	MemWorkSetData []uint64
}

// ResourceConstraint specifies constraint on resources.
type ResourceConstraint struct {
	CPUConstraint    float64 `json: cpuConstraint`
	MemoryConstraint uint64  `json: memoryConstraint`
}

// SingleContainerSummary is a resource usage summary for a single container.
type SingleContainerSummary struct {
	Name string
	Cpu  float64
	Mem  uint64
}

type uint64arr []uint64

func (a uint64arr) Len() int           { return len(a) }
func (a uint64arr) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a uint64arr) Less(i, j int) bool { return a[i] < a[j] }

// ComputePercentiles calculates percentiles for given data series.
func ComputePercentiles(timeSeries []ResourceUsagePerContainer, percentilesToCompute []int) map[int]ResourceUsagePerContainer {
	if len(timeSeries) == 0 {
		return make(map[int]ResourceUsagePerContainer)
	}
	dataMap := make(map[string]*UsageDataPerContainer)
	for i := range timeSeries {
		for name, data := range timeSeries[i] {
			if dataMap[name] == nil {
				dataMap[name] = &UsageDataPerContainer{
					CpuData:        make([]float64, 0, len(timeSeries)),
					MemUseData:     make([]uint64, 0, len(timeSeries)),
					MemWorkSetData: make([]uint64, 0, len(timeSeries)),
				}
			}
			dataMap[name].CpuData = append(dataMap[name].CpuData, data.CPUUsageInCores)
			dataMap[name].MemUseData = append(dataMap[name].MemUseData, data.MemoryUsageInBytes)
			dataMap[name].MemWorkSetData = append(dataMap[name].MemWorkSetData, data.MemoryWorkingSetInBytes)
		}
	}
	for _, v := range dataMap {
		sort.Float64s(v.CpuData)
		sort.Sort(uint64arr(v.MemUseData))
		sort.Sort(uint64arr(v.MemWorkSetData))
	}

	result := make(map[int]ResourceUsagePerContainer)
	for _, perc := range percentilesToCompute {
		data := make(ResourceUsagePerContainer)
		for k, v := range dataMap {
			percentileIndex := int(math.Ceil(float64(len(v.CpuData)*perc)/100)) - 1
			data[k] = &ContainerResourceUsage{
				Name:                    k,
				CPUUsageInCores:         v.CpuData[percentileIndex],
				MemoryUsageInBytes:      v.MemUseData[percentileIndex],
				MemoryWorkingSetInBytes: v.MemWorkSetData[percentileIndex],
			}
		}
		result[perc] = data
	}
	return result
}

// LeftMergeData merges two data structures.
func LeftMergeData(left, right map[int]ResourceUsagePerContainer) map[int]ResourceUsagePerContainer {
	result := make(map[int]ResourceUsagePerContainer)
	for percentile, data := range left {
		result[percentile] = data
		if _, ok := right[percentile]; !ok {
			continue
		}
		for k, v := range right[percentile] {
			result[percentile][k] = v
		}
	}
	return result
}
