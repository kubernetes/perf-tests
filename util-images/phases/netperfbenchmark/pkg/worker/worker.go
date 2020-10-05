package worker

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
)

// var (
// 	iperf3Path = "/usr/local/bin/iperf3"
// 	iperf2Path = "/usr/local/bin/iperf"
// 	siegePath  = "/usr/local/bin/siege"
// )

var listener net.Listener
var resultCh = make(chan string, 100)

//WorkerRPC service that exposes ExecTestcase, GetPerfMetrics API for clients
type WorkerRPC int

func (w *WorkerRPC) Metrics(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In metrics")
	// listener.Close()
	result := make([]string, 20)
	close(resultCh)
	for v := range resultCh {
		result = append(result, v)
	}
	stringSlices := strings.Join(result[:], "\n\n")
	klog.Info("Metrics:", stringSlices)
	return nil
}

func (w *WorkerRPC) Stop(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In stop")
	listener.Close()
	return nil
}

func (w *WorkerRPC) StartTCPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartTCPClient")
	go execCmd(tc.Duration, "iperf3", []string{"-c", tc.DestinationIP, "-l", "20", "-b", "1M", "-i", "1"})
	return nil
}

func (w *WorkerRPC) StartTCPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartTCPServer")
	go execCmd(tc.Duration, "iperf3", []string{"-s", "-i", "1"})
	return nil
}

func (w *WorkerRPC) StartUDPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	//iperf -s -u -e -i 1
	klog.Info("In StartUDPServer")
	go execCmd(tc.Duration, "iperf", []string{"-s", "-u", "-e", "-i", "1", "-t"})
	return nil
}

func (w *WorkerRPC) StartUDPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	//iperf -c localhost -u -l 20 -b 1M -e -i 1
	klog.Info("In StartUDPClient")
	go execCmd(-1, "iperf", []string{"-c", tc.DestinationIP, "-u", "-l", "20", "-b", "1M", "-e", "-i", "1"})
	return nil
}

func (w *WorkerRPC) StartHTTPServer(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartHTTPServer")
	// //mux := http.NewServeMux()
	// //http.DefaultServeMux = mux
	// // mux.HandleFunc("/", Handler)
	http.HandleFunc("/test", Handler)
	// http.ListenAndServe(":5001", nil)
	// klog.Info("http server shut")
	listener1, err := net.Listen("tcp", ":"+api.HttpPort)
	if err != nil {
		klog.Error("Siege server listen error:", err)
	}
	go http.Serve(listener1, nil)
	return nil
}

func (w *WorkerRPC) StartHTTPClient(tc *api.WorkerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartHTTPClient")
	go execCmd(-1, "siege", []string{"http://" + tc.DestinationIP + ":" + api.HttpPort + "/test", "-d1",
		"-r" + strconv.Itoa(tc.Duration), "-c1"})
	return nil
}

func Handler(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(res, "hi\n")
}

// func execCmd2(path string, args []string, duration ...string) {
// 	cmd := exec.Command(path, args...)
// 	out, err := cmd.CombinedOutput()
// 	if err != nil {
// 		klog.Error("failed executing ", path, args, err)
// 	}
// 	klog.Info("Output:" + string(out))
// }

func execCmd(duration int, path string, args []string) {
	cmd := exec.Command(path, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		klog.Error("unable to obtain Stdout:", err)
	}
	eout, err := cmd.StderrPipe()
	if err != nil {
		klog.Error("unable to obtain Stderr:", err)
	}
	multiRdr := io.MultiReader(out, eout)
	// scanner := bufio.NewScanner(out)
	go scanOutput(&multiRdr)
	cmd.Start()
}

func scanOutput(out *io.Reader) {
	scanner := bufio.NewScanner(*out)
	klog.Info("Starting scan for output")
	for scanner.Scan() {
		line := scanner.Text()
		// klog.Info(line)
		resultCh <- line
	}
	klog.Info("Command executed,sending result back")
	if err := scanner.Err(); err != nil {
		klog.Error("Error", err)
	}
}

func initializeServerRPC() {
	baseObject := new(WorkerRPC)
	err := rpc.Register(baseObject)
	if err != nil {
		klog.Fatalf("failed to register rpc", err)
	}
	rpc.HandleHTTP()
	var e error
	listener, e = net.Listen("tcp", ":"+api.WorkerRpcSvcPort)
	if e != nil {
		klog.Fatalf("listen error:", e)
	}
	klog.Info("About to serve rpc...")
	go startServer(&listener)
	klog.Info("Started http server")

	//TODO to be removed
	test()
}

//TODO to be removed , test method
func test() {
	//TODO to be removed ,test///////////////////////
	client, err := rpc.DialHTTP("tcp", "localhost"+":"+api.WorkerRpcSvcPort)
	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}
	podData := &api.WorkerRequest{DestinationIP: "localhost", Duration: 5}
	var reply api.WorkerResponse
	metricReq := &api.MetricRequest{}
	var metricRes api.MetricResponse
	//TCP TEST
	err = client.Call("WorkerRPC.StartTCPServer", podData, &reply)
	time.Sleep(2 * time.Second)
	err = client.Call("WorkerRPC.StartTCPClient", podData, &reply)
	time.Sleep(15 * time.Second)
	client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//UDP TEST
	resultCh = make(chan string, 40)
	err = client.Call("WorkerRPC.StartUDPServer", podData, &reply)
	time.Sleep(2 * time.Second)
	err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	time.Sleep(15 * time.Second)
	client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//HTTP test
	resultCh = make(chan string, 40)
	err = client.Call("WorkerRPC.StartHTTPServer", podData, &reply)
	time.Sleep(2 * time.Second)
	err = client.Call("WorkerRPC.StartHTTPClient", podData, &reply)
	time.Sleep(15 * time.Second)
	client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	klog.Info("TESTING COMPLETED!")
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
	initializeServerRPC()
	register(api.ControllerRpcSvcPort)
}

func register(port string) {
	client, err := rpc.DialHTTP("tcp", api.ControllerHost+":"+port)
	if err != nil {
		klog.Error("dialing:", err)
		return
		//TODO WHAT IF FAILS?
	}

	//Not checking the presence of env vars as it's better in any case for cntrlr to know
	podData := &api.WorkerPodData{os.Getenv(api.PodName), os.Getenv(api.NodeName),
		os.Getenv(api.PodIP), os.Getenv(api.ClusterIp)}
	var reply api.WorkerPodRegReply
	err = client.Call("ControllerRPC.RegisterWorkerPod", podData, &reply)
}
