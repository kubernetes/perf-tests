package network

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

var workerPodList map[string][]WorkerPodData

var podPairCh = make(chan UniquePodPair)

var firstClientPodTime int64

const initialDelayForTCExec = 5

var metricVal map[string][]float64
var uniqPodPairList []UniquePodPair
var K8sClient clientset.Interface

//var metricDataCh = make(chan NetworkPerfResp)

//Client-To-Server Pod ratio indicator
const (
	OneToOne   = "1:1"
	ManyToOne  = "N:1"
	ManyToMany = "N:M"
)

const (
	TCP_Server = iota
	TCP_Client
	UDP_Server
	UDP_Client
	HTTP_Server
	HTTP_Client
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

var httpPathMap = map[int]string{
	TCP_Server:  "startTCPServer",
	TCP_Client:  "WorkerRPC.StartTCPClient",
	UDP_Server:  "WorkerRPC.StartUDPServer",
	UDP_Client:  "WorkerRPC.StartUDPClient",
	HTTP_Server: "WorkerRPC.StartHTTPServer",
	HTTP_Client: "WorkerRPC.StartHTTPClient",
}

const (
	Throughput   = "Throughput"
	Latency      = "Latency"
	Jitter       = "Jitter"
	PPS          = "Packet_Per_Second"
	ResponseTime = "Response_Time"
)

type NetworkPerfResp struct {
	Client_Server_Ratio string
	Protocol            string
	Service             string
	DataItems           []DataItem
}

var metricUnitMap = map[string]string{
	Throughput:   "kbytes/sec",
	Latency:      "ms",
	Jitter:       "ms",
	PPS:          "pps",
	ResponseTime: "seconds",
}

const manifestsPathPrefix = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/measurement/common/network/manifests/*.yaml"

// DataItem is the data point.
type DataItem struct {
	Data   map[string]float64 `json:"data"`
	Unit   string             `json:"unit"`
	Labels map[string]string  `json:"labels,omitempty"`
}

func Start(clientIfc clientset.Interface, namespace string) {
	workerPodList = make(map[string][]WorkerPodData)
	metricVal = make(map[string][]float64)
	K8sClient = clientIfc
	namespace = namespace
}

func populateWorkerPodList(data *WorkerPodData) error {
	if podData, ok := workerPodList[data.WorkerNode]; !ok {
		workerPodList[data.WorkerNode] = []WorkerPodData{{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp}}
		return nil
	} else {
		workerPodList[data.WorkerNode] = append(podData, WorkerPodData{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp})
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
	case ManyToOne:
		executeManyToOneTest(clientPodNum, serverPodNum, duration, protocol)
	case ManyToMany:
		executeManyToManyTest(duration, protocol)
	default:
		klog.Error("Invalid Pod Ratio")
	}
}

//Select one client , one server pod.
func executeOneToOneTest(duration int, protocol string) {
	var uniqPodPair UniquePodPair

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

	//sleep till test-run
	time.Sleep(time.Duration(duration+initialDelayForTCExec+3) * time.Second)
	var metricResp MetricResponse
	collectMetrics(uniqPodPair, protocol, &metricResp)
	populateMetricValMap(uniqPodPair, protocol, &metricResp)
	calculateAndSendMetricVal(protocol, OneToOne)
}

//Select N clients , one server pod.
func executeManyToOneTest(clientPodNum int, serverPodNum int, duration int, protocol string) {

}

//Select N clients , M server pod.
func executeManyToManyTest(duration int, protocol string) {
	var uniqPodPair UniquePodPair
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

	//sleep till test-run
	time.Sleep(time.Duration(duration+initialDelayForTCExec+3) * time.Second)
	collectMetricForManyToMany(protocol)
	calculateAndSendMetricVal(protocol, ManyToMany)
}

func formUniquePodPair(originalMap *map[string][]WorkerPodData) {
	var uniqPodPair UniquePodPair
	lastPodPair := UniquePodPair{IsLastPodPair: true}

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
				uniqPodPair.SrcPodIp = unUsedPod.PodIp
				uniqPodPair.SrcPodName = unUsedPod.PodName
			} else if i == 2 {
				uniqPodPair.DestPodIp = unUsedPod.PodIp
				uniqPodPair.DestPodName = unUsedPod.PodName
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

func getUnusedPod(unusedPodList *[]WorkerPodData) (WorkerPodData, error) {
	var unusedPod WorkerPodData
	if len(*unusedPodList) == 0 {
		return unusedPod, errors.New("Unused pod list empty")
	}
	numOfPods := len(*unusedPodList)
	//extract last pod of slice
	unusedPod = (*unusedPodList)[numOfPods-1]
	*unusedPodList = (*unusedPodList)[:numOfPods-1]
	return unusedPod, nil
}

func sendReqToClient(uniqPodPair UniquePodPair, protocol string, duration int, futureTimestamp int64) {
	klog.Info("Unique pod pair client:", uniqPodPair)
	//clientReq := &ClientRequest{Duration: duration, DestinationIP: uniqPodPair.DestPodIp, Timestamp: futureTimestamp}
	//klog.Info("Client req:", clientReq)
	switch protocol {
	case Protocol_TCP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[TCP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	case Protocol_UDP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[UDP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	case Protocol_HTTP:
		StartWork(uniqPodPair.SrcPodName, httpPathMap[HTTP_Client], duration, futureTimestamp, "", uniqPodPair.DestPodIp)
	}
}

func sendReqToSrv(uniqPodPair UniquePodPair, protocol string, duration int) {
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

func collectMetrics(uniqPodPair UniquePodPair, protocol string, metricResp *MetricResponse) {
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
	metricResp = FetchMetrics(podName)
}

func collectMetricForManyToMany(protocol string) {
	var metricResp MetricResponse
	for _, podPair := range uniqPodPairList {
		collectMetrics(podPair, protocol, &metricResp)
		populateMetricValMap(podPair, protocol, &metricResp)
		klog.Info("Metrics Response from worker:", metricResp)
	}
}

//For TCP,UDP the metrics are collected from ServerPod.
//For HTTP, the metrics are collected from clientPod
func populateMetricValMap(uniqPodPair UniquePodPair, protocol string, metricResp *MetricResponse) {
	switch protocol {
	case Protocol_TCP:
		//metricVal[uniqPodPair.DestPodName] = (*metricResp).Result
		fallthrough
	case Protocol_UDP:
		metricVal[uniqPodPair.DestPodName] = (*metricResp).Result
	case Protocol_HTTP:
		metricVal[uniqPodPair.SrcPodName] = (*metricResp).Result
	}
}

func calculateAndSendMetricVal(protocol string, podRatioType string) {
	var metricData NetworkPerfResp
	switch protocol {
	case Protocol_TCP:
		getMetricData(&metricData, podRatioType, TCPBW, Throughput)
		metricData.Protocol = Protocol_TCP
	case Protocol_UDP:
		getMetricData(&metricData, podRatioType, UDPPps, PPS)
		getMetricData(&metricData, podRatioType, UDPJitter, Jitter)
		getMetricData(&metricData, podRatioType, UDPLatAvg, Latency)
		metricData.Protocol = Protocol_UDP
	case Protocol_HTTP:
		getMetricData(&metricData, podRatioType, HTTPRespTime, ResponseTime)
		metricData.Protocol = Protocol_HTTP
	}
	metricData.Service = "P2P"
	metricData.Client_Server_Ratio = podRatioType
	//metricDataCh <- metricData
}

func getMetricData(data *NetworkPerfResp, podRatioType string, metricIndex int, metricName string) {
	var dataElem DataItem
	dataElem.Data = make(map[string]float64)
	dataElem.Labels = make(map[string]string)
	dataElem.Labels["Metric"] = metricName
	calculateMetricDataValue(&dataElem, podRatioType, metricIndex)
	dataElem.Unit = getUnit(dataElem.Labels["Metric"])
	data.DataItems = append(data.DataItems, dataElem)
	klog.Infof("data:%v", data)
}

func calculateMetricDataValue(dataElem *DataItem, podRatioType string, metricIndex int) {
	var aggrPodPairMetricSlice []float64
	resultSlice := make([]float64, 10)
	for _, resultSlice = range metricVal {
		aggrPodPairMetricSlice = append(aggrPodPairMetricSlice, resultSlice[metricIndex])
	}
	klog.Info("Metric Index:", metricIndex, " AggregatePodMetrics:", aggrPodPairMetricSlice)
	switch podRatioType {
	case OneToOne:
		dataElem.Data[value] = resultSlice[metricIndex]
	case ManyToMany:
		dataElem.Data[Perc95] = getPercentile(aggrPodPairMetricSlice, Percentile95)
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

func messageWorker(podName string, params map[string]string, msgType string) *[]byte {
	req := K8sClient.CoreV1().RESTClient().Get().
		Namespace("netperf-1").
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
