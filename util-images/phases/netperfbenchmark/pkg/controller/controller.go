package controller

import (
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"

	"k8s.io/klog"
)

var globalLock sync.Mutex

//ControllerRPC service that exposes RegisterWorkerPod API for clients
type ControllerRPC int

var workerPodList map[string][]api.WorkerPodData

const ratioSeparator = ":"

//Client-To-Server Pod ratio indicator
const (
	OneToOne = 1
	ManyToOne = 2
	ManyToMany = 3
	InvalidRatio = 4
)

func Start() {
	workerPodList = make(map[string][]api.WorkerPodData)
	InitializeServerRPC(api.ControllerRpcSvcPort)
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
	err = http.Serve(listener, nil)
	if err != nil {
		klog.Fatalf("failed start server", err)
	}
}

func (t *ControllerRPC) RegisterWorkerPod(data *api.WorkerPodData, reply *api.WorkerPodRegReply) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	if podData, ok := workerPodList[data.WorkerNode] ; !ok {
		workerPodList[data.WorkerNode] = []api.WorkerPodData{{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp}}
		reply.PodName = ""
		reply.WorkerNode = ""
		reply.ControllerPodIp = ""
		reply.ControllerRpcPort = api.ControllerRpcSvcPort
		return nil
	} else {
		workerPodList[data.WorkerNode] = append(podData, api.WorkerPodData{PodName: data.PodName, WorkerNode: data.WorkerNode, PodIp: data.PodIp})
		return nil
	}
}

func ExecuteTest(ratio string, duration string, protocol string) {
	var clientPodNum , serverPodNum, ratioType int

	//Get the client-server pod numbers
	clientPodNum , serverPodNum, ratioType = deriveClientServerPodNum(ratio)

	switch ratioType {
	case OneToOne:
		executeOneToOneTest(strconv.Atoi(duration), protocol)
	case ManyToOne:
		executeManyToOneTest()
	case ManyToMany:
		executeManyToManyTest()
	default:
		klog.Fatalf("Invalid Pod Ratio")
	}
}

func deriveClientServerPodNum(ratio string) (int, int, int) {
	var podNumber []string
	var clientPodNum , serverPodNum, ratioType int
	if string.Contains(ratio, ratioSeparator) {
		podNumber = string.Split(ratio, ratioSeparator)
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

	klog.Fatalf("Invalid Pod Ratio")
	return -1,-1,InvalidRatio
}

func extractPodRatio(podNumber []string) int {
	var clientPodNum , serverPodNum int
	clientPodNum, _ = strconv.Atoi(podNumber[0])
	serverPodNum, _ = strconv.Atoi(podNumber[1])
	if clientPodNum == serverPodNum {
		return OneToOne
	}
	if (clientPodNum > serverPodNum) && serverPodNum == 1 {
		return ManyToOne
	}
	if clientPodNum > 1 && serverPodNum > 1 {
		return ManyToMany
	}
	return InvalidRatio
}

//Select one client , one server pod.
func executeOneToOneTest(duration int, protocol string) {

	if len(workerPodList) == 0 || len(workerPodList) == 1 {
		klog.Fatalf("Either no worker-node or one worker node exist. Can't choose pods from same worker node")
		return
	}

}

