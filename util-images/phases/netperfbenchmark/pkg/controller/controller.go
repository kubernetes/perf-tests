package controller

import (
	"errors"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/klog"
)

//ControllerRPC service that exposes RegisterWorkerPod API for clients
type ControllerRPC int

var workerPodList map[string][]api.WorkerPodData
var syncWait *sync.WaitGroup
var globalLock sync.Mutex

var podPairCh = make(chan api.UniquePodPair)

var firstClientPodTime int64

const initialDelayForTCExec = 5

//Client-To-Server Pod ratio indicator
const (
	OneToOne   = 1
	ManyToOne  = 2
	ManyToMany = 3
)

const (
	TCP_Server = iota
	TCP_Client
	UDP_Server
	UDP_Client
	HTTP_Server
	HTTP_Client
)

var protocolRpcMap = map[int]string{
	TCP_Server:  "WorkerRPC.StartTCPServer",
	TCP_Client:  "WorkerRPC.StartTCPClient",
	UDP_Server:  "WorkerRPC.StartUDPServer",
	UDP_Client:  "WorkerRPC.StartUDPClient",
	HTTP_Server: "WorkerRPC.StartHTTPServer",
	HTTP_Client: "WorkerRPC.StartHTTPClient",
}

func Start(ratio string) {
	workerPodList = make(map[string][]api.WorkerPodData)

	// Use WaitGroup to ensure all client pods registration
	// with controller pod.
	syncWait = new(sync.WaitGroup)
	clientPodNum, _, _ := deriveClientServerPodNum(ratio)
	syncWait.Add(clientPodNum)

	InitializeServerRPC(api.ControllerRpcSvcPort)
}

func startServer(listener *net.Listener) {
	err := http.Serve(*listener, nil)
	if err != nil {
		klog.Info("failed start server", err)
	}
	klog.Info("Stopping rpc")
}

func InitializeServerRPC(port string) {
	baseObject := new(ControllerRPC)
	err := rpc.Register(baseObject)
	if err != nil {
		klog.Fatalf("failed to register rpc", err)
	}
	rpc.HandleHTTP()
	listener, e := net.Listen("tcp", ":"+port)
	if e != nil {
		klog.Fatalf("listen error:", e)
	}
	klog.Info("About to serve rpc...")
	go startServer(&listener)
	klog.Info("Started http server")
}

func WaitForWorkerPodReg() {
	// This Blocks the execution
	// until its counter become 0
	syncWait.Wait()
}

func (t *ControllerRPC) RegisterWorkerPod(data *api.WorkerPodData, reply *api.WorkerPodRegReply) error {
	globalLock.Lock()
	defer globalLock.Unlock()
	defer syncWait.Done()

	if podData, ok := workerPodList[data.WorkerNode]; !ok {
		workerPodList[data.WorkerNode] = []api.WorkerPodData{{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp}}
		reply.Response = "Hi"
		return nil
	} else {
		workerPodList[data.WorkerNode] = append(podData, api.WorkerPodData{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp})
		return nil
	}
}

func deriveClientServerPodNum(ratio string) (int, int, int) {
	var podNumber []string
	var clientPodNum, serverPodNum, ratioType int
	if strings.Contains(ratio, api.RatioSeparator) {
		podNumber = strings.Split(ratio, api.RatioSeparator)
		clientPodNum, _ = strconv.Atoi(podNumber[0])
		serverPodNum, _ = strconv.Atoi(podNumber[1])

		if clientPodNum == serverPodNum {
			ratioType = OneToOne
		}
		if (clientPodNum > serverPodNum) && serverPodNum == 1 {
			ratioType = ManyToOne
		}
		if clientPodNum > 1 && serverPodNum > 1 {
			ratioType = ManyToMany
		}
		return clientPodNum, serverPodNum, ratioType
	}

	return -1, -1, -1
}

func ExecuteTest(ratio string, duration string, protocol string) {
	var clientPodNum, serverPodNum, ratioType int

	clientPodNum, serverPodNum, ratioType = deriveClientServerPodNum(ratio)

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
func executeOneToOneTest(duration string, protocol string) {
	var uniqPodPair api.UniquePodPair

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
	timeDuration, _ := strconv.Atoi(duration)
	time.Sleep(time.Duration(timeDuration) * time.Second)
	collectMetrics(uniqPodPair, protocol)
}

//Select N clients , one server pod.
func executeManyToOneTest(clientPodNum int, serverPodNum int, duration string, protocol string) {

}

//Select N clients , M server pod.
func executeManyToManyTest(duration string, protocol string) {
	var uniqPodPair api.UniquePodPair
	var endOfPodPairs = false
	var podPairIndex = 0

	go formUniquePodPair(&workerPodList)

	for {
		select {
		case uniqPodPair = <-podPairCh:
			sendReqToSrv(uniqPodPair, protocol, duration)
			time.Sleep(50 * time.Millisecond)
			//Get timestamp for first pair and use the same for all
			if podPairIndex == 0 {
				firstClientPodTime = getTimeStampForPod()
			}
			sendReqToClient(uniqPodPair, protocol, duration, firstClientPodTime)
			podPairIndex++
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

func formUniquePodPair(originalMap *map[string][]api.WorkerPodData) {
	var uniqPodPair api.UniquePodPair
	lastPodPair := api.UniquePodPair{IsLastPodPair: true}

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
				podPairCh <- uniqPodPair
			}
		}
		if len(*originalMap) == 0 {
			podPairCh <- lastPodPair
			break
		}
	}
}

func getUnusedPod(unusedPodList *[]api.WorkerPodData) (api.WorkerPodData, error) {
	var unusedPod api.WorkerPodData
	if len(*unusedPodList) == 0 {
		return unusedPod, errors.New("Unused pod list empty")
	}
	numOfPods := len(*unusedPodList)
	//extract last pod of slice
	unusedPod = (*unusedPodList)[numOfPods-1]
	*unusedPodList = (*unusedPodList)[:numOfPods-1]
	return unusedPod, nil
}

func sendReqToClient(uniqPodPair api.UniquePodPair, protocol string, duration string, futureTimestamp int64) {
	client, err := rpc.DialHTTP("tcp", uniqPodPair.SrcPodIp+":"+api.WorkerRpcSvcPort)
	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}

	clientReq := &api.ClientRequest{Duration: duration, DestinationIP: uniqPodPair.DestPodIp, Timestamp: futureTimestamp}

	switch protocol {
	case api.Protocol_TCP:
		clientRPCMethod(client, protocolRpcMap[TCP_Client], clientReq)
	case api.Protocol_UDP:
		clientRPCMethod(client, protocolRpcMap[UDP_Client], clientReq)
	case api.Protocol_HTTP:
		clientRPCMethod(client, protocolRpcMap[HTTP_Client], clientReq)
	}

}

func sendReqToSrv(uniqPodPair api.UniquePodPair, protocol string, duration string) {
	client, err := rpc.DialHTTP("tcp", uniqPodPair.DestPodIp+":"+api.WorkerRpcSvcPort)
	if err != nil {
		klog.Fatalf("dialing:", err)
	}

	serverReq := &api.ServerRequest{Duration: duration, NumClients: "1", Timestamp: time.Now().Unix()}

	switch protocol {
	case api.Protocol_TCP:
		serverRPCMethod(client, protocolRpcMap[TCP_Server], serverReq)
	case api.Protocol_UDP:
		serverRPCMethod(client, protocolRpcMap[UDP_Server], serverReq)
	case api.Protocol_HTTP:
		serverRPCMethod(client, protocolRpcMap[HTTP_Server], serverReq)
	}
}

func getTimeStampForPod() int64 {
	currTime := time.Now()
	initDelayInSec := time.Second * time.Duration(initialDelayForTCExec)
	futureTime := currTime.Add(initDelayInSec).Unix()
	return futureTime
}

func collectMetrics(uniqPodPair api.UniquePodPair, protocol string) {
	var err error
	var client *rpc.Client

	switch protocol {
	case api.Protocol_TCP:
	case api.Protocol_UDP:
		client, err = rpc.DialHTTP("tcp", uniqPodPair.DestPodIp+":"+api.WorkerRpcSvcPort)

	case api.Protocol_HTTP:
		client, err = rpc.DialHTTP("tcp", uniqPodPair.SrcPodIp+":"+api.WorkerRpcSvcPort)
	}

	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}

	metricReq := &api.MetricRequest{}
	metricRPCMethod(client, metricReq)
}

func metricRPCMethod(client *rpc.Client, metricReq *api.MetricRequest) {
	var reply api.WorkerResponse
	rpcMethod := "WorkerRPC.Metrics"
	err := client.Call(rpcMethod, *metricReq, &reply)
	if err != nil {
		klog.Error("RPC call to : %s failed with err: %s", rpcMethod, err)
	}
}

func clientRPCMethod(client *rpc.Client, rpcMethod string, clientReq *api.ClientRequest) {
	var reply api.WorkerResponse
	err := client.Call(rpcMethod, *clientReq, &reply)
	if err != nil {
		klog.Error("RPC call to client : %s failed with err: %s", rpcMethod, err)
	}
}

func serverRPCMethod(client *rpc.Client, rpcMethod string, clientReq *api.ServerRequest) {
	var reply api.WorkerResponse
	err := client.Call(rpcMethod, *clientReq, &reply)
	if err != nil {
		klog.Error("RPC call to server : %s failed with err: %s", rpcMethod, err)
	}
}
