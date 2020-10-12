package worker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/phases/netperfbenchmark/api"
)

var listener net.Listener
var resultCh = make(chan string, 100)
var resultStatus = make(chan string, 1)

var iperfUDPFn = []string{"", "", "", "Sum", "Sum", "Sum", "Sum", "Sum", "Avg", "Avg", "Min", "Max", "Avg", "Sum"}
var iperfTCPFn = []string{"Sum", "Avg"}

//WorkerRPC service that exposes ExecTestcase, GetPerfMetrics API for clients
type WorkerRPC int

func (w *WorkerRPC) Metrics(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In metrics")
	var status string
	// listener.Close()
	select {
	case status = <-resultStatus:
		if status != "OK" {
			klog.Error("Error collecting metrics:", status)
			return errors.New("metrics collection failed:" + status)
		}
		klog.Info("Metrics collected")
	default:
		klog.Info("Metric collection in progress")
		return errors.New("Metrics in progress")
	}
	result := make([]string, 0)
	close(resultCh) //can be removed,if  following exact time based closure
	for v := range resultCh {
		result = append(result, v)
	}

	//TODO to be deleted, for debugging
	for i, v := range result {
		klog.Info("le", i, ":", v)
	}
	output, err := parseResult(result)
	if err != nil {
		return err
	}
	reply.Result = output
	return nil
	// stringSlices := strings.Join(result[:], "\n\n")
}

func (w *WorkerRPC) Stop(tc *api.MetricRequest, reply *api.MetricResponse) error {
	klog.Info("In stop")
	listener.Close()
	return nil
}

func (w *WorkerRPC) StartTCPClient(tc *api.ClientRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartTCPClient")
	// go execCmd(tc.Duration, "iperf3", []string{"-c", tc.DestinationIP, "-f", "K", "-l", "20", "-b", "1M", "-i", "1"})
	go execCmd(tc.Duration, "iperf", []string{"-c", tc.DestinationIP, "-f", "K", "-l", "20", "-b", "1M", "-i", "1", "-t", tc.Duration})
	return nil
}

func (w *WorkerRPC) StartTCPServer(tc *api.ServerRequest, reply *api.WorkerResponse) error {
	klog.Info("In StartTCPServer")
	resultCh <- "TCP"
	// go execCmd(tc.Duration, "iperf3", []string{"-s", "-f", "K", "-i", "1"})
	go execCmd(tc.Duration, "iperf", []string{"-s", "-f", "K", "-i", tc.Duration, "-P", tc.NumClients})
	return nil
}

func (w *WorkerRPC) StartUDPClient(tc *api.ClientRequest, reply *api.WorkerResponse) error {
	//iperf -c localhost -u -l 20 -b 1M -e -i 1
	klog.Info("In StartUDPClient")
	go execCmd(tc.Duration, "iperf", []string{"-c", tc.DestinationIP, "-u", "-f", "K", "-l", "20", "-b", "1M", "-e", "-i", "1", "-t", tc.Duration})
	return nil
}

func (w *WorkerRPC) StartUDPServer(tc *api.ServerRequest, reply *api.WorkerResponse) error {
	//iperf -s -u -e -i <duration> -P <num parallel clients>
	klog.Info("In StartUDPServer")
	resultCh <- "UDP"
	go execCmd(tc.Duration, "iperf", []string{"-s", "-f", "K", "-u", "-e", "-i", tc.Duration, "-P", tc.NumClients})
	return nil
}

func (w *WorkerRPC) StartHTTPClient(tc *api.ClientRequest, reply *api.WorkerResponse) error {
	//// siege http://localhost:5301/test -d1 -r1 -c1 -t10S
	//c concurrent r repetitions t time d delay in sec between 1 and d
	resultCh <- "HTTP"
	klog.Info("In StartHTTPClient")
	go execCmd(tc.Duration, "siege",
		[]string{"http://" + tc.DestinationIP + ":" + api.HttpPort + "/test",
			"-d1", "-t" + tc.Duration + "S", "-c1"})
	return nil
}

func (w *WorkerRPC) StartHTTPServer(tc *api.ServerRequest, reply *api.WorkerResponse) error {
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

func execCmd(duration string, command string, args []string) {
	cmd := exec.Command(command, args...)
	resultCh <- duration
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
		if line == "" {
			continue
		}
		resultCh <- line
	}
	klog.Info("Command executed,sending result back")
	if err := scanner.Err(); err != nil {
		klog.Error("Error", err)
		resultStatus <- err.Error()
	} else {
		resultStatus <- "OK"
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
	podData := &api.ClientRequest{DestinationIP: "localhost", Duration: "10", Timestamp: 1}
	var reply api.WorkerResponse
	metricReq := &api.MetricRequest{}
	var metricRes api.MetricResponse
	// TCP TEST
	// err = client.Call("WorkerRPC.StartTCPServer", podData, &reply)
	// time.Sleep(2 * time.Second)
	// err = client.Call("WorkerRPC.StartTCPClient", podData, &reply)
	// time.Sleep(15 * time.Second)
	// client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//UDP TEST
	//resultCh = make(chan string, 140)
	// err = client.Call("WorkerRPC.StartUDPServer", podData, &reply)
	// // time.Sleep(2 * time.Second)
	// err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	// err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	// time.Sleep(15 * time.Second)
	// client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//HTTP test
	// // resultCh = make(chan string, 40)
	err = client.Call("WorkerRPC.StartHTTPServer", podData, &reply)
	time.Sleep(2 * time.Second)
	err = client.Call("WorkerRPC.StartHTTPClient", podData, &reply)
	time.Sleep(15 * time.Second)
	client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//klog.Info("TESTING COMPLETED!")
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
	klog.Info("Env variables:", "POD_NAME:"+os.Getenv(api.PodName), " NODE_NAME:"+os.Getenv(api.NodeName),
		" POD_IP:"+os.Getenv(api.PodIP), " CLUSTER_IP:"+os.Getenv(api.ClusterIp))
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

func parseResult(result []string) ([]float64, error) {
	klog.Info("Parsing", result[0])
	switch result[0] {
	case "TCP":
		return parseTCP(result), nil
	case "UDP":
		return parseUDP(result), nil
	case "HTTP":
		return parseHTTP(result), nil
	default:
		return nil, errors.New("wrong result type:" + result[0])

	}
	return nil, nil

}

func parseTCP(result []string) []float64 {
	klog.Info("In Parse iperf")
	dur := result[1]
	fmtResult := make([][]string, 0)
	sumResult := make([]float64, 0)
	unitReg := regexp.MustCompile(`\[\s+|\]\s+|KBytes\s+|KBytes/sec\s*|sec\s+|ms\s+|us\s*`)
	mulSpaceReg := regexp.MustCompile(`\s+`)
	cnt := 0
	sessionId := make(map[string]bool)
	for _, op := range result {
		klog.Info(op)
		if !strings.Contains(op, "0.0-"+dur+".0") {
			continue
		}
		//TODO try using one regex
		frmtString := mulSpaceReg.ReplaceAllString(unitReg.ReplaceAllString(op, " "), " ")
		klog.Info("Trim info:", frmtString)
		split := strings.Split(frmtString, " ")
		fmtResult = append(fmtResult, split)
		//for bug in iperf tcp
		if len(split) >= 3 && "SUM" != split[1] && split[2] == "0.0-"+dur+".0" { //if the record is for the complete duration of run
			if _, ok := sessionId[split[1]]; ok {
				continue
			}
			for i, v := range split {
				klog.Info("Split", i, ":", v)
				if i == 1 {
					sessionId[v] = true
					continue
				}
				if i == 0 || i == 2 || i == 5 { //first index and hte last is ""
					continue
				}
				tmp, err := strconv.ParseFloat(v, 64)
				if err != nil {
					klog.Error("conversion error", err)
				}
				if len(sumResult) <= 1 {
					sumResult = append(sumResult, tmp)
				} else {
					switch iperfTCPFn[i-3] {
					case "Sum":
						sumResult[i-3] = tmp + sumResult[i-3]
					case "Avg":
						sumResult[i-3] = (float64(cnt)*tmp + sumResult[i-3]) / (float64(1 + cnt))
					case "Min":
						sumResult[i-3] = math.Min(tmp, sumResult[i-3])
					case "Max":
						sumResult[i-3] = math.Max(tmp, sumResult[i-3])
					}
				}
				cnt++
			}
		}
	}
	klog.Info("Matrix:", fmtResult)
	klog.Info("Final output:", sumResult)
	return sumResult

}

func parseUDP(result []string) []float64 {
	klog.Info("In Parse iperf")
	dur := result[1]
	fmtResult := make([][]string, 0)
	sumResult := make([]float64, 0)
	unitReg := regexp.MustCompile(`%|\[\s+|\]\s+|KBytes\s+|KBytes/sec\s+|sec\s+|pps\s*|ms\s+|/|\(|\)\s+`)
	mulSpaceReg := regexp.MustCompile(`\s+`)
	cnt := 0
	for _, op := range result {
		if !strings.Contains(op, "0.00-"+dur+".00") {
			continue
		}
		//TODO try using one regex
		frmtString := mulSpaceReg.ReplaceAllString(unitReg.ReplaceAllString(op, " "), " ")
		klog.Info("Trim info:", frmtString)
		split := strings.Split(frmtString, " ")
		fmtResult = append(fmtResult, split)
		if len(split) >= 13 && "SUM" != split[1] && split[2] == "0.00-"+dur+".00" { //if the record is for the complete duration of run
			for i, v := range split {
				klog.Info("Split", i, ":", v)
				if i == 0 || i == 1 || i == 2 || i == 14 { //first index and hte last is ""
					continue
				}
				tmp, err := strconv.ParseFloat(v, 64)
				if err != nil {
					klog.Error("conversion error", err)
				}
				if len(sumResult) < 11 {
					sumResult = append(sumResult, tmp)
				} else {
					switch iperfUDPFn[i] {
					case "Sum":
						sumResult[i-3] = tmp + sumResult[i-3]
					case "Avg":
						sumResult[i-3] = (float64(cnt)*tmp + sumResult[i-3]) / (float64(1 + cnt))
					case "Min":
						sumResult[i-3] = math.Min(tmp, sumResult[i-3])
					case "Max":
						sumResult[i-3] = math.Max(tmp, sumResult[i-3])
					}
				}
				cnt++
			}
		}
	}
	klog.Info("Matrix:", fmtResult)
	klog.Info("Final output:", sumResult)
	return sumResult
}

func parseHTTP(result []string) []float64 {
	canAppend := false
	sumResult := make([]float64, 0)
	mulSpaceReg := regexp.MustCompile(`\s+`)
	for _, op := range result {
		if canAppend != true && strings.HasPrefix(op, "Transactions:") {
			canAppend = true
		}
		if canAppend == false {
			continue
		}
		fmtStr := mulSpaceReg.ReplaceAllString(op, " ")
		split := strings.Split(fmtStr, ":")
		klog.Info("Formatted:", fmtStr)
		if len(split) > 1 {
			split := strings.Split(split[1], " ")
			if len(split) < 2 {
				continue
			}
			tmp, err := strconv.ParseFloat(split[1], 64)
			if err != nil {
				klog.Error("Error parsing:", err)
			}
			sumResult = append(sumResult, tmp)
		}

		if strings.HasPrefix(op, "Shortest transaction") {
			break
		}
	}
	klog.Info("Final output:", sumResult)
	return sumResult
}
