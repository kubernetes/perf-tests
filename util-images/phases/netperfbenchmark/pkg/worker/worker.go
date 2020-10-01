package worker

import (
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
)

// var (
// 	iperf3Path = "/usr/local/bin/iperf3"
// 	iperf2Path = "/usr/local/bin/iperf"
// 	siegePath  = "/usr/local/bin/siege"
// )

var listener net.Listener

//WorkerRPC service that exposes ExecTestcase, GetPerfMetrics API for clients
type WorkerRPC int

func (w *WorkerRPC) Metrics(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In metrics")
	// listener.Close()
	return nil
}

func (w *WorkerRPC) Stop(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In stop")
	listener.Close()
	return nil
}

func (w *WorkerRPC) StartTCPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {

	return nil
}

func (w *WorkerRPC) StartTCPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartTCPServer")
	go execCmd("iperf3", []string{"-s"})
	return nil
}

func (w *WorkerRPC) StartUDPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	//iperf -s -u -e -i 1
	klog.Info("In StartUDPServer")
	go execCmd("iperf", []string{"-s", "-u", "-e", "-i", "1", "-t", tc.Duration})
	return nil
}

func (w *WorkerRPC) StartUDPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	//iperf -c localhost -u -l 20 -b 1M -e -i 1
	klog.Info("In StartUDPClient")
	go execCmd("iperf", []string{"-c", tc.DestinationIP, "-u", "-l", "20", "-b", "1M", "-e", "-i", "1"})
	return nil
}

func (w *WorkerRPC) StartHTTPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func (w *WorkerRPC) StartHTTPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	return nil
}

func execCmd(path string, args []string) {
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		klog.Error("failed executing %s", cmd.String, err)
	}
	klog.Info("Output:" + string(out))
}

func initializeServerRPC(port string) {
	baseObject := new(WorkerRPC)
	err := rpc.Register(baseObject)
	if err != nil {
		klog.Fatalf("failed to register rpc", err)
	}
	rpc.HandleHTTP()
	var e error
	listener, e = net.Listen("tcp", ":"+port)
	if e != nil {
		klog.Fatalf("listen error:", e)
	}
	klog.Info("About to serve rpc...")
	go startServer(&listener)
	klog.Info("Started http server")

	//TODO to be removed ,test///////////////////////
	// client, err := rpc.DialHTTP("tcp", "localhost"+":"+port)
	// if err != nil {
	// 	klog.Fatalf("dialing:", err)
	// 	//TODO WHAT IF FAILS?
	// }
	// podData := &api.WorkerRequest{DestinationIP: "localhost", Duration: "13"}
	// var reply api.WorkerResponse
	// // podData := &api.MetricRequest{}
	// // var reply api.MetricResponse
	// // err = client.Call("WorkerRPC.Metrics", podData, &reply)
	// // err = client.Call("WorkerRPC.Stop", podData, &reply)
	// err = client.Call("WorkerRPC.StartUDPServer", podData, &reply)
	// time.Sleep(2 * time.Second)
	// err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	////////////////////////////////////////////////

}

func startServer(listener *net.Listener) {
	err := http.Serve(*listener, nil)
	if err != nil {
		klog.Info("failed start server", err)
	}
	klog.Info("Stopping rpc")
}

func Start() {
	initializeServerRPC(api.WorkerRpcSvcPort)
	register(api.ControllerRpcSvcPort)
}

func register(port string) {
	client, err := rpc.DialHTTP("tcp", api.ControllerHost+":"+port)
	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}

	//Not checking the presence of env vars as it's better in any case for cntrlr to know
	podData := &api.WorkerPodData{os.Getenv(api.PodName), os.Getenv(api.NodeName),
		os.Getenv(api.PodIP), os.Getenv(api.ClusterIp)}
	var reply api.WorkerPodRegReply
	err = client.Call("ControllerRPC.RegisterWorkerPod", podData, &reply)
}
