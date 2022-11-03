/*
Copyright 2021 The Kubernetes Authors.

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

package network

import (
	"container/heap"
	"fmt"

	"k8s.io/klog/v2"
)

// workerNodeInfo represents node in heap
type workerNodeInfo struct {
	nodeName     string
	numberOfPods int
}

type nodeHeap []*workerNodeInfo

func (nh nodeHeap) Len() int { return len(nh) }
func (nh nodeHeap) Less(i, j int) bool {
	return nh[i].numberOfPods > nh[j].numberOfPods
}

func (nh nodeHeap) Swap(i, j int) {
	nh[i], nh[j] = nh[j], nh[i]
}

func (nh *nodeHeap) Push(x interface{}) {
	*nh = append(*nh, x.(*workerNodeInfo))
}

func (nh *nodeHeap) Pop() interface{} {
	old := *nh
	n := len(old)
	x := old[n-1]
	*nh = old[0 : n-1]
	return x
}

// workerPodData represents details of pods running on worker node
type workerPodData struct {
	podName    string
	workerNode string
	podIP      string
}

type podList []workerPodData

func (l *podList) Pop() (*workerPodData, error) {
	if len(*l) == 0 {
		return nil, fmt.Errorf("empty pod list")
	}
	lastElement := (*l)[len(*l)-1]
	*l = (*l)[:len(*l)-1]
	return &lastElement, nil
}

// podPair represents source-destination worker pod pair.
type podPair struct {
	index              int
	sourcePodName      string
	sourcePodIP        string
	destinationPodName string
	destinationPodIP   string
}

func newPodPair(index int, sourcePod, destinationPod *workerPodData) podPair {
	return podPair{
		index:              index,
		sourcePodName:      sourcePod.podName,
		sourcePodIP:        sourcePod.podIP,
		destinationPodName: destinationPod.podName,
		destinationPodIP:   destinationPod.podIP,
	}
}

type workerPodInfo struct {
	workerPodMap map[string]podList
}

func (wp *workerPodInfo) formUniquePodPair() []podPair {
	nh := make(nodeHeap, 0, len(wp.workerPodMap))
	podPairList := make([]podPair, 0)
	for key, value := range wp.workerPodMap {
		nh = append(nh, &workerNodeInfo{nodeName: key, numberOfPods: len(value)})
	}
	heap.Init(&nh)
	for i := 0; nh.Len() > 1; i++ {
		firstNode := heap.Pop(&nh).(*workerNodeInfo)
		secondNode := heap.Pop(&nh).(*workerNodeInfo)
		if firstNode.numberOfPods > 1 {
			heap.Push(&nh, &workerNodeInfo{nodeName: firstNode.nodeName, numberOfPods: firstNode.numberOfPods - 1})
		}
		if secondNode.numberOfPods > 1 {
			heap.Push(&nh, &workerNodeInfo{nodeName: secondNode.nodeName, numberOfPods: secondNode.numberOfPods - 1})
		}

		sourcePod, err1 := wp.pop(firstNode.nodeName)
		destinationPod, err2 := wp.pop(secondNode.nodeName)
		if err1 != nil || err2 != nil {
			klog.Errorf("error in popping pods. err1: %v, err2: %v", err1, err2)
			continue
		}
		podPairList = append(podPairList, newPodPair(i, sourcePod, destinationPod))
	}
	return podPairList
}

func (wp *workerPodInfo) pop(nodeName string) (*workerPodData, error) {
	podList, ok := wp.workerPodMap[nodeName]
	if !ok {
		return nil, fmt.Errorf("worker node %s is removed", nodeName)
	}
	pod, err := podList.Pop()
	if err != nil {
		delete(wp.workerPodMap, nodeName)
		return nil, err
	}
	wp.workerPodMap[nodeName] = podList
	return pod, nil
}
