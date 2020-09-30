package worker

import (
	"net"
	"net/http"
	"net/rpc"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
)

//WorkerRPC service that exposes ExecTestcase, GetPerfMetrics API for clients
type WorkerRPC int

func (w *WorkerRPC) Metrics(tc *api.MetricRequest, reply *api.MetricResponse) error {
	return nil
}

func (w *WorkerRPC) startTCPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) startTCPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) startUDPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) startUDPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) startHTTPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) startHTTPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func InitializeServerRPC(port string) {
	baseObject := new(WorkerRPC)
	err := rpc.Register(&baseObject)
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

func Start() {
	InitializeServerRPC(api.WorkerRpcSvcPort)
	register(api.ControllerRpcSvcPort)
}

func register(port string) {
	client, err := rpc.DialHTTP("tcp", api.ControllerHost+":"+port)
	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}
	podData := &api.WorkerPodData{}
	var reply api.WorkerPodRegReply
	err = client.Call("ControllerRPC.RegisterWorkerPod", podData, &reply)
}
