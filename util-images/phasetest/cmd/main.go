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
	//"encoding/json"
	//"log"

	"encoding/json"
	"log"

	//"encoding/json"
	"errors"
	"flag"
	//"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"

	//"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
	"math"
	"sort"
	"time"

	//"fmt"
	//"time"

	"k8s.io/klog"
	//measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

//const (
//	Millisec     = "ms"
//	HTTP         = "HTTP"
//	ResponseTime = "ResponseTime"
//)

//var (
//	mode     = flag.String("mode", "", "Mode that should be run. Supported values: controller or worker")
//	ratio    = flag.String("client-server-pod-ratio", "", "Client POD to Server POD ratio")
//	duration = flag.String("measurement-duration", "", "Duration of metric collection in seconds")
//	protocol = flag.String("protocol", "", "Protocol to be tested. Supported values: tcp or or udp or http")
//)

type uniquePodPair struct {
	srcPodName    string
	srcPodIp      string
	destPodName   string
	destPodIp     string
	IsLastPodPair bool `default: false`
}

type workerPodData struct {
	podName    string
	workerNode string
	podIp      string
	clusterIP  string
}

var workerPodList map[string][]workerPodData
var podPairCh = make(chan uniquePodPair)
var podPairStatus = make(chan string)

const initialDelayForTCExec = 5

var firstClientPodTime time.Time

func init() {
	workerPodList = make(map[string][]workerPodData)
	metricVal = make(map[string][]float64)
}

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	klog.Infof("Starting main \n")
	registerDataPoint("W-1", "W1-P1", "W-1", "10.1.1.1", "20.1.1.1")
	registerDataPoint("W-1", "W1-P2", "W-1", "10.1.1.2", "20.1.1.1")
	registerDataPoint("W-1", "W1-P3", "W-1", "10.1.1.3", "20.1.1.1")

	registerDataPoint("W-2", "W2-P1", "W-2", "10.1.2.1", "20.1.2.1")
	registerDataPoint("W-2", "W2-P2", "W-2", "10.1.2.2", "20.1.2.1")

	registerDataPoint("W-3", "W3-P1", "W-3", "10.1.3.1", "20.1.3.1")
	registerDataPoint("W-3", "W3-P2", "W-3", "10.1.3.2", "20.1.3.1")
	registerDataPoint("W-3", "W3-P3", "W-3", "10.1.3.3", "20.1.3.1")

	klog.Infof("Size of map: %d \n", len(workerPodList))

	//formUniquePodPair(&workerPodList)
	//klog.Infof("Parent worker list: %s \n", workerPodList)
	//klog.Infof("---------------------1st-Iteration----------------------------")
	//
	//subGrp1 := make(map[string][]workerPodData)
	//subGrp2 := make(map[string][]workerPodData)

	//divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	//klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	//klog.Infof("SubGrp1 map: %s \n", subGrp1)
	//
	//klog.Infof("SubGrp2 map: %s \n", subGrp2)

	//klog.Infof("Parent worker list: %s \n", workerPodList)
	//
	//klog.Infof("---------------------2nd-Iteration----------------------------")

	//divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	//klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	//klog.Infof("SubGrp1 map: %s \n", subGrp1)
	//
	//klog.Infof("SubGrp2 map: %s \n", subGrp2)
	//
	//klog.Infof("Parent worker list: %s \n", workerPodList)
	//
	//klog.Infof("---------------------3rd-Iteration----------------------------")

	//divideMapIntoSubGrp(&workerPodList, &subGrp1, &subGrp2)

	//klog.Infof("Unique podPair : %s \n", getUniquePodPair(&subGrp1, &subGrp2))

	//klog.Infof("SubGrp1 map: %s \n", subGrp1)
	//
	//klog.Infof("SubGrp2 map: %s \n", subGrp2)

	klog.Infof("-------------------------------------------------")

	//now := time.Now()
	//
	//fmt.Println("Now is : ", now.Format(time.ANSIC))
	//
	//// get three minutes into future
	//threeSec := time.Second * time.Duration(3)
	//future := now.Add(threeSec)
	//fmt.Println("Three sec from now will be : ", future.Format(time.ANSIC))
	//
	//t1 := time.Now()
	//t2 := t1.Add(time.Second * 341)
	//
	//fmt.Println(t1)
	//fmt.Println(t2)
	//
	//diff := t2.Sub(t1)
	//fmt.Println(diff)

	klog.Infof("-------------------------------------------------")

	//getTimeStampForPod(firstPodTime, 0)
	//klog.Infof("currTime : %s", time.Now().Format(time.StampMilli))
	//klog.Infof(getTimeStampForPod(firstClientPodTime, 0))
	//
	//time.Sleep(500 * time.Millisecond)
	//klog.Infof(getTimeStampForPod(firstClientPodTime, 1))
	//
	//time.Sleep(500 * time.Millisecond)
	//klog.Infof(getTimeStampForPod(firstClientPodTime, 2))

	//executeManyToManyTest()

	//var jsonData []byte
	//jsonData, err := json.Marshal(ToPerfData("sec", "Response_Time"))
	//if err != nil {
	//	log.Println(err)
	//}
	//klog.Infof("PerfData : %s", string(jsonData))

	//var values = []float64{2, 3, 4, 6, 7, 9, 11, 13}
	//klog.Infof("Percentile : %v", getPercentile(values, 0.95))

	var uniqPodPar1 = UniquePodPair{"W1-P1", "", "W2-P2", "", false}
	var uniqPodPar2 = UniquePodPair{"W2-P1", "", "W1-P2", "", false}
	var uniqPodPar3 = UniquePodPair{"W4-P1", "", "W3-P2", "", false}
	var uniqPodPar4 = UniquePodPair{"W4-P2", "", "W3-P1", "", false}

	var metricResp1 = MetricResponse{Result: []float64{10, 200}}
	var metricResp2 = MetricResponse{Result: []float64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100}}
	var metricResp3 = MetricResponse{Result: []float64{3100, 3200, 3300, 3400, 3500, 3600, 3700, 3800, 3900, 31000, 31100}}
	var metricResp4 = MetricResponse{Result: []float64{4100, 4200, 4300, 4400, 4500, 4600, 4700, 4800, 4900, 41000, 41100}}
	populateMetricValMap(uniqPodPar1, Protocol_TCP, &metricResp1)
	populateMetricValMap(uniqPodPar2, Protocol_UDP, &metricResp2)
	populateMetricValMap(uniqPodPar3, Protocol_HTTP, &metricResp3)
	populateMetricValMap(uniqPodPar4, Protocol_UDP, &metricResp4)

	go printjsondata()

	calculateAndSendMetricVal(Protocol_UDP, 1)
	//calculateAndSendMetricVal(Protocol_TCP, 1)
	//calculateAndSendMetricVal(Protocol_HTTP, 1)

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

//func divideMapIntoSubGrp(originalMap *map[string][]workerPodData, subGrp1 *map[string][]workerPodData, subGrp2 *map[string][]workerPodData) {
//	var i int = 0
//
//	for key, podList := range *originalMap {
//		klog.Infof("podList: %s \n", podList)
//		if len(podList) == 0 {
//			continue
//		}
//		if i%2 == 0 {
//			(*subGrp1)[key] = podList
//		} else {
//			(*subGrp2)[key] = podList
//		}
//		i++
//	}
//
//}

//func getUniquePodPair(subGrp1 *map[string][]workerPodData, subGrp2 *map[string][]workerPodData) uniquePodPair {
//	var srcPod, destPod workerPodData
//	var podPair uniquePodPair
//
//	for key, value := range *subGrp1 {
//		srcPod, _ = getUnusedPod(&value)
//		(*subGrp1)[key] = value
//		//klog.Infof("111 getUniquePodPair key: %s value : %s \n", key, value)
//	}
//
//	for key, value := range *subGrp2 {
//		destPod, _ = getUnusedPod(&value)
//		(*subGrp2)[key] = value
//		//klog.Infof("222 getUniquePodPair key: %s value : %s \n", key, value)
//	}
//
//	podPair.srcPodName = srcPod.podName
//	podPair.srcPodIp = srcPod.podIp
//	podPair.destPodIp = destPod.podIp
//	podPair.destPodName = destPod.podName
//
//	return podPair
//}

//func formUniquePodPair(originalMap *map[string][]workerPodData) {
//	var uniqPodPair uniquePodPair
//	var i int = 0
//
//	//maplen := len(*originalMap)
//
//	for {
//		for key, value := range *originalMap {
//			//klog.Infof("originalMap: %s \n", *originalMap)
//			unUsedPod, err := getUnusedPod(&value)
//			(*originalMap)[key] = value
//			if err != nil {
//				delete(*originalMap, key)
//				continue
//			}
//			i++
//			//klog.Infof("key :%s  value: %s \n", key, value)
//
//			if i == 1 {
//				uniqPodPair.srcPodIp = unUsedPod.podIp
//				uniqPodPair.srcPodName = unUsedPod.podName
//			} else if i == 2 {
//				uniqPodPair.destPodIp = unUsedPod.podIp
//				uniqPodPair.destPodName = unUsedPod.podName
//				i = 0
//				klog.Infof("------------------------------------\n")
//				klog.Infof("===> uniqPodPair: %s \n", uniqPodPair)
//				klog.Infof("-------------------------------------\n")
//			}
//		}
//
//		if len(*originalMap) == 0 {
//			break
//		}
//	}
//}

func getUnusedPod(unusedPodList *[]workerPodData) (workerPodData, error) {
	var unusedPod workerPodData

	if len(*unusedPodList) == 0 {
		return unusedPod, errors.New("Unused pod list empty")
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

func formUniquePodPair(originalMap *map[string][]workerPodData) {
	var uniqPodPair uniquePodPair
	lastPodPair := uniquePodPair{IsLastPodPair: true}

	var i, k = 0, 0
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
				uniqPodPair.srcPodIp = unUsedPod.podIp
				uniqPodPair.srcPodName = unUsedPod.podName
			} else if i == 2 {
				uniqPodPair.destPodIp = unUsedPod.podIp
				uniqPodPair.destPodName = unUsedPod.podName
				i = 0
				podPairCh <- uniqPodPair
			}
		}
		k++
		klog.Infof(" Iter : %d , originalMap: %s \n", k, *originalMap)
		if len(*originalMap) == 0 {
			podPairCh <- lastPodPair
			break
		}
	}
}

func executeManyToManyTest() {
	var uniqPodPair uniquePodPair
	var endOfPodPairs = false

	go formUniquePodPair(&workerPodList)

	for {
		select {
		case uniqPodPair = <-podPairCh:
			klog.Infof(" uniqPodPair: %s \n", uniqPodPair)
			if uniqPodPair.IsLastPodPair {
				endOfPodPairs = true
				break
			}
		default:
			//do nothing
		}
		if endOfPodPairs {
			break
		}
	}

}

func getTimeStampForPod(firstPodTime time.Time, podPairIndex int) string {
	//If first pod pair , just add initial delay
	if podPairIndex == 0 {
		currTime := time.Now()
		initDelayInSec := time.Second * time.Duration(initialDelayForTCExec)
		future := currTime.Add(initDelayInSec)
		//fmt.Println("initDelayInSec will be : ", future.Format(time.StampMilli))
		firstClientPodTime = future
		//klog.Infof("firstClientPodTime : %s , index: %d", firstClientPodTime, podPairIndex)
		return future.Format(time.StampMilli)
	}

	//klog.Infof("222-firstClientPodTime : %s , index: %d", firstPodTime, podPairIndex)
	//If subsequent pod pair, adjust the timediff for initial delay
	currTime := time.Now()
	diff := firstPodTime.Sub(currTime)
	//klog.Infof("diff : %s ", diff)
	//initDelayInSec := time.Second * time.Duration(diff)
	//klog.Infof("diff : %s ", diff)
	future := currTime.Add(diff)
	klog.Infof("index: %d, future: %s ", podPairIndex, future.Format(time.StampMilli))
	return future.Format(time.StampMilli)
}

//func (metric *LatencyMetric) ToPerfData(name string) DataItem {
//	return DataItem{
//		Data: map[string]float64{
//			"Perc50": float64(metric.Perc50) / float64(time.Millisecond),
//			"Perc90": float64(metric.Perc90) / float64(time.Millisecond),
//			"Perc99": float64(metric.Perc99) / float64(time.Millisecond),
//		},
//		Unit: "ms",
//		Labels: map[string]string{
//			"Metric": name,
//		},
//	}
//}

//const (
//	average  = "avg"
//	minimum  = "min"
//	maxmimum = "max"
//)
//
//const (
//	UDPLatAvg = iota
//	UDPLatMin
//	UDPLatMax
//	UDPJitter
//	UDPPps
//	HTTPRespTime
//	TCPThroughput
//)

//func ToPerfData(unitName string, metricName string) measurementutil.DataItem {
//	return measurementutil.DataItem{
//		Data: getDataMap(),
//		Unit: unitName,
//		Labels: map[string]string{
//			"Metric": metricName,
//		},
//	}
//}

//---------------------------------------------------------------
type UniquePodPair struct {
	SrcPodName    string
	SrcPodIp      string
	DestPodName   string
	DestPodIp     string
	IsLastPodPair bool `default: false`
}

type MetricResponse struct {
	Result []float64
}

type metricData struct {
	dataItemArr []dataItems
}

type dataItems struct {
	Data   map[string]float64
	Labels map[string]string
}

type DataItem struct {
	// Data is a map from bucket to real data point (e.g. "Perc90" -> 23.5). Notice
	// that all data items with the same label combination should have the same buckets.
	Data map[string]float64 `json:"data"`
	// Unit is the data unit. Notice that all data items with the same label combination
	// should have the same unit.
	Unit string `json:"unit"`
	// Labels is the labels of the data item.
	Labels map[string]string `json:"labels,omitempty"`
}

const (
	Throughput   = "Throughput"
	Latency      = "Latency"
	Jitter       = "Jitter"
	PPS          = "Packet_Per_Second"
	ResponseTime = "Response_Time"
)

const (
	OneToOne   = 1
	ManyToOne  = 2
	ManyToMany = 3
)

var metricUnitMap = map[string]string{
	Throughput:   "kbytes/sec",
	Latency:      "ms",
	Jitter:       "ms",
	PPS:          "second",
	ResponseTime: "second",
}

const (
	Protocol_TCP  = "tcp"
	Protocol_UDP  = "udp"
	Protocol_HTTP = "http"
)

//TCP result array Index mapping
const (
	TCPTransfer = iota
	TCPBW
)

//UDP result array Index mapping
const (
	UDPTransfer = iota
	UDPBW
	UDPJitter
	UDPLostPkt
	UDPTotalPkt
	UDPLatPer
	UDPLatAvg
	UDPLatMin
	UDPLatMax
	UDPLatStdD
	UDPPps
)

//HTTP result array Index mapping
const (
	HTTPTxs = iota
	HTTPAvl
	HTTPTimeElps
	HTTPDataTrsfd
	HTTPRespTime
	HTTPTxRate
	HTTPThroughput
	HTTPConcurrency
	HTTPTxSuccesful
	HTTPFailedTxs
	HTTPLongestTx
	HTTPShortestTx
)

const (
	Percentile90 = 0.90
	Percentile95 = 0.95
	Percentile99 = 0.99
)

const (
	Perc90 = "Perc90"
	Perc95 = "Perc95"
	Perc99 = "Perc99"
	Min    = "min"
	Max    = "max"
	Avg    = "avg"
	value  = "value"
)

var metricVal map[string][]float64
var metricDataCh = make(chan metricData)
var aggrPodPairMetricSlice []float64

func populateMetricValMap(uniqPodPair UniquePodPair, protocol string, metricResp *MetricResponse) {
	switch protocol {
	case Protocol_TCP:
	case Protocol_UDP:
		metricVal[uniqPodPair.DestPodName] = (*metricResp).Result
	case Protocol_HTTP:
		metricVal[uniqPodPair.SrcPodName] = (*metricResp).Result
	}
}

func calculateAndSendMetricVal(protocol string, podRatio int) {
	var metricData metricData
	switch protocol {
	case Protocol_TCP:
		getMetricData(&metricData, podRatio, TCPBW, Throughput)
	case Protocol_UDP:
		getMetricData(&metricData, podRatio, UDPPps, PPS)
		getMetricData(&metricData, podRatio, UDPJitter, Jitter)
		getMetricData(&metricData, podRatio, UDPLatAvg, Latency)
	case Protocol_HTTP:
		getMetricData(&metricData, podRatio, HTTPRespTime, ResponseTime)
	}
	klog.Infof("sending metricData : %v", metricData)
	metricDataCh <- metricData
}

func getMetricData(data *metricData, podRatioType int, metricIndex int, metricName string) {
	var dataElem dataItems
	dataElem.Data = make(map[string]float64)
	dataElem.Labels = make(map[string]string)
	dataElem.Labels["Metric"] = metricName
	calculateMetricDataValue(&dataElem, podRatioType, metricIndex)
	data.dataItemArr = append(data.dataItemArr, dataElem)
	klog.Infof("data:%v", data)
}

func calculateMetricDataValue(dataElem *dataItems, podRatioType int, metricIndex int) {
	resultSlice := make([]float64, 10)
	for _, resultSlice = range metricVal {
		aggrPodPairMetricSlice = append(aggrPodPairMetricSlice, resultSlice[metricIndex])
	}
	switch podRatioType {
	case OneToOne:
		dataElem.Data[value] = resultSlice[metricIndex]
		klog.Infof("calculateMetricDataValue :OneToOne: resultSlice:%v", resultSlice)
	case ManyToMany:
		dataElem.Data[Perc95] = getPercentile(aggrPodPairMetricSlice, Percentile95)
	}
	klog.Infof("calculateMetricDataValue  dataElem:%v", dataElem)
}

func printjsondata() {
	klog.Infof("before recvd metricData")
	data := <-metricDataCh
	klog.Infof("recvd metricData : %v", data)
	var jsonData []byte
	jsonData, err := json.Marshal(populateDataItem(data))
	if err != nil {
		log.Println(err)
	}
	klog.Infof("PerfData : %s", string(jsonData))
}

func populateDataItem(data metricData) []DataItem {
	var dataItemArr []DataItem

	for _, dataElem := range data.dataItemArr {
		dataItemArr = append(dataItemArr, DataItem{
			Data:   dataElem.Data,
			Unit:   getUnit(dataElem.Labels["Metric"]),
			Labels: dataElem.Labels,
		})
	}
	return dataItemArr
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
