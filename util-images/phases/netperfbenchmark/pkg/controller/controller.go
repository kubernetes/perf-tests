package controller

import (
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog"
)

//ControllerRPC service that exposes RegisterWorkerPod API for clients
type ControllerRPC int

var workerPodList map[string][]api.WorkerPodData
var syncWait *sync.WaitGroup
var globalLock sync.Mutex

//Client-To-Server Pod ratio indicator
const (
	OneToOne   = 1
	ManyToOne  = 2
	ManyToMany = 3
)

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
	timeduration, _ := strconv.Atoi(duration)

	clientPodNum, serverPodNum, ratioType = deriveClientServerPodNum(ratio)

	switch ratioType {
	case OneToOne:
		executeOneToOneTest(timeduration, protocol)
	case ManyToOne:
		executeManyToOneTest(clientPodNum, serverPodNum, timeduration, protocol)
	case ManyToMany:
		executeManyToManyTest(clientPodNum, serverPodNum, timeduration, protocol)
	default:
		klog.Fatalf("Invalid Pod Ratio")
	}
}

//Select one client , one server pod.
func executeOneToOneTest(duration int, protocol string) {
	var nodeNum int

	if len(workerPodList) == 1 {
		klog.Error("Woker pods exist on same worker-node. Not executing Tc")
		return
	}

	//nodeNum = len(workerPodList)
	// Choose two unique worker-nodes first

}

//Select N clients , one server pod.
func executeManyToOneTest(clientPodNum int, serverPodNum int, duration int, protocol string) {

}

//Select N clients , M server pod.
func executeManyToManyTest(clientPodNum int, serverPodNum int, duration int, protocol string) {

}
