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

package main

import (
	"flag"

	"k8s.io/klog"
)

//var (
//	mode     = flag.String("mode", "", "Mode that should be run. Supported values: controller or worker")
//	ratio    = flag.String("client-server-pod-ratio", "", "Client POD to Server POD ratio")
//	duration = flag.String("measurement-duration", "", "Duration of metric collection in seconds")
//	protocol = flag.String("protocol", "", "Protocol to be tested. Supported values: tcp or or udp or http")
//)

type uniquePodPair struct {
	srcPodName  string
	srcPodIp    string
	destPodName string
	destPodIp   string
}

type workerPodData struct {
	podName    string
	workerNode string
	podIp      string
	clusterIP  string
}

var workerPodList map[string][]workerPodData

type podMapType map[string][]workerPodData

func init() {
	workerPodList = make(map[string][]workerPodData)
}

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	klog.Infof("Starting main \n")
	registerDataPoint("worker-1", "W1-P1", "W-1", "10.1.1.1", "20.1.1.1")
	registerDataPoint("worker-1", "W1-P2", "W-1", "10.1.1.2", "20.1.1.1")
	registerDataPoint("worker-1", "W1-P3", "W-1", "10.1.1.3", "20.1.1.1")

	registerDataPoint("worker-2", "W2-P1", "W-2", "10.1.2.1", "20.1.2.1")
	registerDataPoint("worker-2", "W2-P2", "W-2", "10.1.2.2", "20.1.2.1")

	registerDataPoint("worker-3", "W3-P1", "W-3", "10.1.3.1", "20.1.3.1")

	klog.Infof("Size of map: %d \n", len(workerPodList))
	//klog.Infof("Parent worker list: %s \n", workerPodList)
	klog.Infof("---------------------1st-Iteration----------------------------")

	subGrp1 := make(map[string][]workerPodData)
	subGrp2 := make(map[string][]workerPodData)

	divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	klog.Infof("SubGrp1 map: %s \n", subGrp1)

	klog.Infof("SubGrp2 map: %s \n", subGrp2)

	klog.Infof("Parent worker list: %s \n", workerPodList)

	klog.Infof("---------------------2nd-Iteration----------------------------")

	divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	klog.Infof("SubGrp1 map: %s \n", subGrp1)

	klog.Infof("SubGrp2 map: %s \n", subGrp2)

	klog.Infof("Parent worker list: %s \n", workerPodList)

	klog.Infof("---------------------3rd-Iteration----------------------------")

	divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	//klog.Infof("SubGrp1 map: %s \n", subGrp1)
	//
	//klog.Infof("SubGrp2 map: %s \n", subGrp2)

	klog.Infof("-------------------------------------------------")

}

func registerDataPoint(nodeName string, podName string, workerNode string, podIp string, clusterIp string) {
	var podList []workerPodData

	podList, ok := workerPodList[nodeName]
	if !ok {
		workerPodList[nodeName] = []workerPodData{{podName: podName, workerNode: workerNode, podIp: podIp, clusterIP: clusterIp}}
	} else {
		workerPodList[nodeName] = append(podList, workerPodData{podName: podName, workerNode: workerNode, podIp: podIp, clusterIP: clusterIp})
	}
}

func divideMapIntoSubGrp(originalMap *map[string][]workerPodData, subGrp1 *map[string][]workerPodData, subGrp2 *map[string][]workerPodData) {
	var i int = 0

	for key, podList := range *originalMap {
		if len(podList) == 0 {
			continue
		}
		if i%2 == 0 {
			(*subGrp1)[key] = podList
		} else {
			(*subGrp2)[key] = podList
		}
		i++
	}

}

func getUniquePodPair(subGrp1 *map[string][]workerPodData, subGrp2 *map[string][]workerPodData) uniquePodPair {
	var srcPod, destPod workerPodData
	var podPair uniquePodPair

	for key, value := range *subGrp1 {
		srcPod, _ = getUnusedPod(&value)
		(*subGrp1)[key] = value
		//klog.Infof("111 getUniquePodPair key: %s value : %s \n", key, value)
	}

	for key, value := range *subGrp2 {
		destPod, _ = getUnusedPod(&value)
		(*subGrp1)[key] = value
		//klog.Infof("222 getUniquePodPair key: %s value : %s \n", key, value)
	}

	podPair.srcPodName = srcPod.podName
	podPair.srcPodIp = srcPod.podIp
	podPair.destPodIp = destPod.podIp
	podPair.destPodName = destPod.podName

	return podPair
}

func getUnusedPod(unusedPodList *[]workerPodData) (workerPodData, error) {
	var unusedPod workerPodData

	if len(*unusedPodList) == 0 {
		return unusedPod, nil
	}

	numOfPods := len(*unusedPodList)

	//klog.Infof("podList: %s \n", podList)
	//extract last pod of slice
	unusedPod = (*unusedPodList)[numOfPods-1]
	//klog.Infof("Last pod of slice: %s \n", unusedPod)
	*unusedPodList = (*unusedPodList)[:numOfPods-1]
	//klog.Infof(" Slice after removing last pod: %s \n", *unusedPodList)
	return unusedPod, nil
}
