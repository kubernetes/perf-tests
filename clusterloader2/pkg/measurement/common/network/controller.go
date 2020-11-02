/*
Copyright 2016 The Kubernetes Authors.

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
	"context"
	"encoding/json"
	"errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"

	"k8s.io/klog"
)

var workerPodList map[string][]workerPodData

var firstClientPodTime int64

const initialDelayForTCExec = 5

var metricVal map[string]MetricResponse
var uniqPodPairList []uniquePodPair
var metricRespPendingList []uniquePodPair
var K8sClient clientset.Interface

var podPairCh = make(chan uniquePodPair)
var networkPerfRespForDisp NetworkPerfResp

func (npm *networkPerfMetricsMeasurement) Start(clientIfc clientset.Interface) {
	workerPodList = make(map[string][]workerPodData)
	metricVal = make(map[string]MetricResponse)
	clientPodNum, _, _ := deriveClientServerPodNum(npm.podRatio)
	uniqPodPairList = make([]uniquePodPair, clientPodNum)
	metricRespPendingList = make([]uniquePodPair, clientPodNum)
	K8sClient = clientIfc
}

func populateWorkerPodList(data *workerPodData) error {
	if podData, ok := workerPodList[data.workerNode]; !ok {
		workerPodList[data.workerNode] = []workerPodData{{podName: data.podName, workerNode: data.workerNode, podIp: data.podIp}}
		return nil
	} else {
		workerPodList[data.workerNode] = append(podData, workerPodData{podName: data.podName, workerNode: data.workerNode, podIp: data.podIp})
		return nil
	}
}

func deriveClientServerPodNum(ratio string) (int, int, string) {
	var podNumber []string
	var clientPodNum, serverPodNum int
	if strings.Contains(ratio, RatioSeparator) {
		podNumber = strings.Split(ratio, RatioSeparator)
		clientPodNum, _ = strconv.Atoi(podNumber[0])
		serverPodNum, _ = strconv.Atoi(podNumber[1])

		if clientPodNum <= 0 || serverPodNum <= 0 {
			klog.Error("Invalid pod numbers")
			return -1, -1, "-1"
		}
		if clientPodNum == serverPodNum && clientPodNum == 1 {
			return clientPodNum, serverPodNum, OneToOne
		}
		if (clientPodNum > serverPodNum) && serverPodNum == 1 {
			return clientPodNum, serverPodNum, ManyToOne
		}
		if clientPodNum == serverPodNum {
			return clientPodNum, serverPodNum, ManyToMany
		}
	}

	return -1, -1, "-1"
}

func ExecuteTest(ratio string, duration int, protocol string) {
	var clientPodNum, serverPodNum int
	var ratioType string
	clientPodNum, serverPodNum, ratioType = deriveClientServerPodNum(ratio)
	klog.Info("clientPodNum:%d ,  serverPodNum: %d, ratioType: %s", clientPodNum, serverPodNum, ratioType)

	switch ratioType {
	case OneToOne:
		executeOneToOneTest(duration, protocol)
	case ManyToMany:
		executeManyToManyTest(duration, protocol)
	default:
		klog.Error("Invalid Pod Ratio")
	}
}

//Select one client , one server pod.
func executeOneToOneTest(duration int, protocol string) {
	var uniqPodPair uniquePodPair

	if len(workerPodList) == 1 {
		klog.Error("Worker pods exist on same worker-node. Not executing Tc")
		return
	}

	go formUniquePodPair(&workerPodList)

	uniqPodPair = <-podPairCh

	sendReqToSrv(uniqPodPair, protocol, duration)
	time.Sleep(50 * time.Millisecond)
	firstClientPodTime = getTimeStampForPod()
	sendReqToClient(uniqPodPair, protocol, duration, firstClientPodTime)
}

//Select N clients , M server pod.
func executeManyToManyTest(duration int, protocol string) {
	var uniqPodPair uniquePodPair
	var endOfPodPairs = false
	var podPairIndex = 0

	go formUniquePodPair(&workerPodList)

	for {
		select {
		case uniqPodPair = <-podPairCh:
			klog.Info("Pod Pairs:", uniqPodPair)
			if uniqPodPair.IsLastPodPair {
				endOfPodPairs = true
				break
			}
			sendReqToSrv(uniqPodPair, protocol, duration)
			time.Sleep(50 * time.Millisecond)
			//Get timestamp for first pair and use the same for all
			if podPairIndex == 0 {
				firstClientPodTime = getTimeStampForPod()
			}
			sendReqToClient(uniqPodPair, protocol, duration, firstClientPodTime)
			podPairIndex++
		default:
			//do nothing
		}
		if endOfPodPairs {
			break
		}
	}

}

func formUniquePodPair(originalMap *map[string][]workerPodData) {
	var uniqPodPair uniquePodPair
	lastPodPair := uniquePodPair{IsLastPodPair: true}

	var i = 0

	for {
		for key, value := range *originalMap {
			unUsedPod, err := getUnusedPod(&value)
			(*originalMap)[key] = value
			if err != nil {
				delete(*originalMap, key)
				continue
			}
			i++

			if i == 1 {
				uniqPodPair.SrcPodIp = unUsedPod.podIp
				uniqPodPair.SrcPodName = unUsedPod.podName
			} else if i == 2 {
				uniqPodPair.DestPodIp = unUsedPod.podIp
				uniqPodPair.DestPodName = unUsedPod.podName
				i = 0
				uniqPodPairList = append(uniqPodPairList, uniqPodPair)
				podPairCh <- uniqPodPair
			}
		}
		if len(*originalMap) == 0 {
			podPairCh <- lastPodPair
			break
		}
	}
}

func getUnusedPod(unusedPodList *[]workerPodData) (workerPodData, error) {
	var unusedPod workerPodData
	if len(*unusedPodList) == 0 {
		return unusedPod, errors.New("Unused pod list empty")
	}
	numOfPods := len(*unusedPodList)
	//extract last pod of slice
	unusedPod = (*unusedPodList)[numOfPods-1]
	*unusedPodList = (*unusedPodList)[:numOfPods-1]
	return unusedPod, nil
}

func sendReqToClient(uniqPodPair uniquePodPair, protocol string, duration int, futureTimestamp int64) {
	klog.Info("Unique pod pair client:", uniqPodPair)
	switch protocol {
	case Protocol_TCP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[TCP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	case Protocol_UDP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[UDP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	case Protocol_HTTP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[HTTP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	}
}

func sendReqToSrv(uniqPodPair uniquePodPair, protocol string, duration int) {
	klog.Info("Unique pod pair server:", uniqPodPair)
	switch protocol {
	case Protocol_TCP:
		StartWork(uniqPodPair.DestPodName, httpPathMap[TCP_Server], duration, time.Now().Unix(), "1", "")
	case Protocol_UDP:
		StartWork(uniqPodPair.DestPodName, httpPathMap[UDP_Server], duration, time.Now().Unix(), "1", "")
	case Protocol_HTTP:
		StartWork(uniqPodPair.DestPodName, httpPathMap[HTTP_Server], duration, time.Now().Unix(), "1", "")
	}
}

func getTimeStampForPod() int64 {
	currTime := time.Now()
	initDelayInSec := time.Second * time.Duration(initialDelayForTCExec)
	futureTime := currTime.Add(initDelayInSec).Unix()
	return futureTime
}

func collectMetrics(uniqPodPair uniquePodPair, protocol string) *MetricResponse {
	var podName string

	switch protocol {
	case Protocol_TCP:
		fallthrough
	case Protocol_UDP:
		podName = uniqPodPair.DestPodName
		klog.Info("[collectMetrics] destPodIp: %s podName: %s", uniqPodPair.DestPodIp, podName)
	case Protocol_HTTP:
		podName = uniqPodPair.SrcPodName
		klog.Info("[collectMetrics] srcPodIp: %s podName: %s", uniqPodPair.SrcPodIp, podName)
	}
	metricResp := FetchMetrics(podName)
	return metricResp
}

//For TCP,UDP the metrics are collected from ServerPod.
//For HTTP, the metrics are collected from clientPod
func populateMetricValMap(uniqPodPair uniquePodPair, protocol string, metricResp *MetricResponse) {
	switch protocol {
	case Protocol_TCP:
		fallthrough
	case Protocol_UDP:
		metricVal[uniqPodPair.DestPodName] = *metricResp
	case Protocol_HTTP:
		metricVal[uniqPodPair.SrcPodName] = *metricResp
	}
	klog.Info("Metric in populateMetricValMap:", *metricResp)
}

func formNetPerfRespForDisp(protocol string, podRatioType string, finalPodRatio float64) NetworkPerfResp {
	var metricData NetworkPerfResp
	switch protocol {
	case Protocol_TCP:
		getMetricData(&metricData, podRatioType, finalPodRatio, TCPBW, Throughput)
		metricData.Protocol = Protocol_TCP
	case Protocol_UDP:
		getMetricData(&metricData, podRatioType, finalPodRatio, UDPPps, PPS)
		getMetricData(&metricData, podRatioType, finalPodRatio, UDPJitter, Jitter)
		getMetricData(&metricData, podRatioType, finalPodRatio, UDPLatAvg, Latency)
		metricData.Protocol = Protocol_UDP
	case Protocol_HTTP:
		getMetricData(&metricData, podRatioType, finalPodRatio, HTTPRespTime, ResponseTime)
		metricData.Protocol = Protocol_HTTP
	}
	metricData.Service = "P2P"
	metricData.Client_Server_Ratio = podRatioType
	return metricData
}

func getMetricData(data *NetworkPerfResp, podRatioType string, finalPodRatio float64, metricIndex int, metricName string) {
	var dataElem measurementutil.DataItem
	dataElem.Data = make(map[string]float64)
	dataElem.Labels = make(map[string]string)
	dataElem.Labels["Metric"] = metricName
	calculateMetricDataValue(&dataElem, podRatioType, metricIndex, finalPodRatio)
	dataElem.Unit = getUnit(dataElem.Labels["Metric"])
	data.DataItems = append(data.DataItems, dataElem)
	klog.Infof("data:%v", data)
}

func calculateMetricDataValue(dataElem *measurementutil.DataItem, podRatioType string, metricIndex int, finalPodRatio float64) {
	var aggrPodPairMetricSlice []float64
	var metricResp MetricResponse
	for _, metricResp = range metricVal {
		aggrPodPairMetricSlice = append(aggrPodPairMetricSlice, metricResp.Result[metricIndex])
	}
	klog.Info("Metric Index:", metricIndex, " AggregatePodMetrics:", aggrPodPairMetricSlice)
	switch podRatioType {
	case OneToOne:
		dataElem.Data[value] = metricResp.Result[metricIndex]
	case ManyToMany:
		dataElem.Data[Perc95] = getPercentile(aggrPodPairMetricSlice, Percentile95)
		dataElem.Data[Num_Pod_Pairs] = finalPodRatio
	}
}

func StartWork(podName string, wrkType string, duration int, timestamp int64,
	numCls string, srvrIP string) {
	var resp WorkerResponse
	var params = make(map[string]string)
	params["duration"] = strconv.Itoa(duration)
	params["timestamp"] = strconv.FormatInt(timestamp, 10)
	params["numCls"] = numCls
	params["destIP"] = srvrIP
	klog.Info("Params:", params)
	klog.Info("POdname:", podName, " workType:", wrkType)
	body := messageWorker(podName, params, wrkType)
	if err := json.Unmarshal(*body, &resp); err != nil {
		klog.Info("Error unmarshalling metric response:", err)
	}
	klog.Info("Unmarshalled response to startWork:", resp)
}

func FetchMetrics(podName string) *MetricResponse {
	var resp MetricResponse
	var params = make(map[string]string)
	body := messageWorker(podName, params, "metrics")
	if err := json.Unmarshal(*body, &resp); err != nil {
		klog.Info("Error unmarshalling metric response:", err)
	}
	klog.Info("Unmarshalled metrics:", resp)
	return &resp
}

func getMetricsForDisplay(podRatio string, protocol string) {
	var metricResp *MetricResponse

	_, _, ratioType := deriveClientServerPodNum(podRatio)
	for _, podPair := range uniqPodPairList {
		metricResp = collectMetrics(podPair, protocol)
		if metricResp != nil || metricResp.Error != "" {
			populateMetricValMap(podPair, protocol, metricResp)
		} else {
			metricRespPendingList = append(metricRespPendingList, podPair)
		}
	}

	wait.Poll(time.Duration(1)*time.Second, time.Duration(5)*time.Second, func() (bool, error) {
		return getMetricsFromPendingPods(protocol)
	})

	actualPodRatio := getActualPodRatioForDisp()
	networkPerfRespForDisp = formNetPerfRespForDisp(protocol, ratioType, actualPodRatio)
}

func getMetricsFromPendingPods(protocol string) (bool, error) {
	var pendingList []uniquePodPair
	var metricResp *MetricResponse

	if len(metricRespPendingList) == 0 {
		return true, nil
	}

	for _, podPair := range metricRespPendingList {
		metricResp = collectMetrics(podPair, protocol)
		if metricResp != nil || metricResp.Error != "" {
			populateMetricValMap(podPair, protocol, metricResp)
		} else {
			pendingList = append(pendingList, podPair)
		}
	}

	metricRespPendingList = pendingList
	return false, nil
}

func getActualPodRatioForDisp() float64 {
	podPairNum := len(uniqPodPairList) - len(metricRespPendingList)
	return float64(podPairNum)
}

func messageWorker(podName string, params map[string]string, msgType string) *[]byte {
	req := K8sClient.CoreV1().RESTClient().Get().
		Namespace(netperfNamespace).
		Resource("pods").
		Name(podName + ":5003").
		SubResource("proxy").Suffix(msgType)
	for k, v := range params {
		req = req.Param(k, v)
	}
	body, err := req.DoRaw(context.TODO())
	if err != nil {
		klog.Info("Error calling ", msgType, ":", err.Error())
	} else {
		klog.Info("GOT RESPONSE:")
		klog.Info(string(body))
	}
	return &body
}

func getUnit(metric string) string {
	return metricUnitMap[metric]
}

type float64Slice []float64

func (p float64Slice) Len() int           { return len(p) }
func (p float64Slice) Less(i, j int) bool { return p[i] < p[j] }
func (p float64Slice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func getPercentile(values float64Slice, perc float64) float64 {
	ps := []float64{perc}

	scores := make([]float64, len(ps))
	size := len(values)
	if size > 0 {
		sort.Sort(values)
		for i, p := range ps {
			pos := p * float64(size+1) //ALTERNATIVELY, DROP THE +1
			if pos < 1.0 {
				scores[i] = float64(values[0])
			} else if pos >= float64(size) {
				scores[i] = float64(values[size-1])
			} else {
				lower := float64(values[int(pos)-1])
				upper := float64(values[int(pos)])
				scores[i] = lower + (pos-math.Floor(pos))*(upper-lower)
			}
		}
	}
	return scores[0]
}
