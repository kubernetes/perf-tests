package worker

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/rpc"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/util-images/network/netperfbenchmark/api"
)

var listener net.Listener
var resultCh = make(chan string, 100)
var resultStatus = make(chan string, 1)
var startedAt int64
var futureTime int64

var iperfUDPFn = []string{"", "", "", "Sum", "Sum", "Sum", "Sum", "Sum", "Avg", "Avg", "Min", "Max", "Avg", "Sum"}
var iperfTCPFn = []string{"Sum", "Avg"}

//WorkerRPC service that exposes ExecTestcase, GetPerfMetrics API for clients
type WorkerRPC int

func Start(controllerIp string) {
	listenToServer()
}

func listenToServer() {
	handlers := map[string]func(http.ResponseWriter, *http.Request){"/startTCPServer": StartTCPServer,
		"/startTCPClient": StartTCPClient, "/startUDPServer": StartUDPServer, "/startUDPClient": StartUDPClient,
		"/startHTTPServer": StartHTTPServer, "/startHTTPClient": StartHTTPClient, "/metrics": Metrics}
	startListening(api.WorkerListenPort, handlers)
	klog.Info("Started listening to Server")
	//TODO to be removed
	// test()
}

func startListening(port string, handlers map[string]func(http.ResponseWriter, *http.Request)) error {
	klog.Info("In StartHTTPServer")
	for path, handler := range handlers {
		http.HandleFunc(path, handler)
	}
	listener1, err := net.Listen("tcp", ":"+port)
	if err != nil {
		klog.Error("Error listening:", err)
	}
	go http.Serve(listener1, nil)
	return nil
}

func Metrics(res http.ResponseWriter, req *http.Request) {
	var reply api.MetricResponse
	klog.Info("In metrics")
	var status string
	select {
	case status = <-resultStatus:
		if status != "OK" {
			klog.Error("Error collecting metrics:", status)
			reply.Error = "metrics collection failed:" + status
			createResp(reply, &res)
			return
		}
		klog.Info("Metrics collected")
	default:
		klog.Info("Metric collection in progress")
		reply.Error = "Metrics in progress"
		createResp(reply, &res)
		return
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
		reply.Error = err.Error()
		createResp(reply, &res)
		return
	}
	reply.Result = output

	st := strconv.FormatInt(startedAt, 10)
	ft := strconv.FormatInt(futureTime, 10)
	dt := strconv.FormatInt(futureTime-startedAt, 10)
	reply.WorkerStartTime = "StartedAt:" + st + " FutureTime:" + ft + " diffTime:" + dt
	createResp(reply, &res)
	// stringSlices := strings.Join(result[:], "\n\n")
}

func createResp(resp interface{}, w *http.ResponseWriter) {
	klog.Info("Inside reply")
	(*w).Header().Set("Content-Type", "application/json")
	(*w).WriteHeader(http.StatusOK)
	klog.Info("Marshalled Resp:", resp)
	b, err := json.Marshal(resp)
	if err != nil {
		klog.Info("Error marshalling to json:", err)
	}
	(*w).Write(b)
}

func Stop(res http.ResponseWriter, req *http.Request) {
	klog.Info("In stop")
	listener.Close()
}

func StartTCPClient(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartTCPClient")
	klog.Info("Req:", req)
	ts, dur, destIP, _ := parseURLParam(req)
	go schedule(ts, dur,
		"iperf", []string{"-c", destIP, "-f", "K", "-l",
			"20", "-b", "1M", "-i", "1", "-t", dur})
}

func StartTCPServer(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartTCPServer")
	klog.Info("Req:", req)
	resultCh <- "TCP"
	ts, dur, _, numcl := parseURLParam(req)
	go schedule(ts, dur, "iperf", []string{"-s", "-f", "K", "-i", dur, "-P", numcl})
}

func StartUDPServer(res http.ResponseWriter, req *http.Request) {
	//iperf -s -u -e -i <duration> -P <num parallel clients>
	klog.Info("In StartUDPServer")
	resultCh <- "UDP"
	ts, dur, _, numcl := parseURLParam(req)
	go schedule(ts, dur, "iperf", []string{"-s", "-f", "K", "-u", "-e", "-i", dur, "-P", numcl})
}

func StartUDPClient(res http.ResponseWriter, req *http.Request) {
	//iperf -c localhost -u -l 20 -b 1M -e -i 1
	klog.Info("In StartUDPClient")
	ts, dur, destIP, _ := parseURLParam(req)
	go schedule(ts, dur, "iperf", []string{"-c", destIP, "-u", "-f", "K", "-l", "20", "-b", "1M", "-e", "-i", "1", "-t", dur})
}

func StartHTTPServer(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartHTTPServer")
	startListening(api.HttpPort, map[string]func(http.ResponseWriter, *http.Request){"/test": Handler})
}

func StartHTTPClient(res http.ResponseWriter, req *http.Request) {
	//// siege http://localhost:5301/test -d1 -r1 -c1 -t10S
	//c concurrent r repetitions t time d delay in sec between 1 and d
	resultCh <- "HTTP"
	klog.Info("In StartHTTPClient")
	ts, dur, destIP, _ := parseURLParam(req)
	go schedule(ts, dur, "siege",
		[]string{"http://" + destIP + ":" + api.HttpPort + "/test",
			"-d1", "-t" + dur + "S", "-c1"})
}

func Handler(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(res, "hi\n")
}

func parseURLParam(req *http.Request) (int64, string, string, string) {
	values := req.URL.Query()
	ts := values.Get("timestamp")
	dur := values.Get("duration")
	destIP := values.Get("destIP")
	numcl := values.Get("numCls")
	var tsint int64
	var err error
	if ts != "" {
		tsint, err = strconv.ParseInt(ts, 10, 64)
		if err != nil {
			klog.Info("Invalid timestamp:", ts, " ", err)
		}
	}
	return tsint, dur, destIP, numcl

}

func schedule(futureTimestamp int64, duration string, command string, args []string) {
	//If future time is in past,run immediately
	klog.Info("About to wait for futuretime:", futureTimestamp)
	klog.Info("Current time:", time.Now().Unix())
	time.Sleep(time.Duration(futureTimestamp-time.Now().Unix()) * time.Second)
	atomic.AddInt64(&startedAt, time.Now().Unix())
	atomic.AddInt64(&futureTime, futureTimestamp)
	execCmd(duration, command, args)
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

//TODO to be removed , test method
func test() {
	//TODO to be removed ,test///////////////////////
	client, err := rpc.DialHTTP("tcp", "localhost"+":"+api.WorkerListenPort)
	if err != nil {
		klog.Fatalf("dialing:", err)
		//TODO WHAT IF FAILS?
	}
	currTime := time.Now()
	initDelayInSec := time.Second * time.Duration(5)
	futureTime := currTime.Add(initDelayInSec).Unix()
	klog.Info("Current timestamp:", time.Now().Unix())
	klog.Info("Schedule timestamp:", futureTime)
	serpodData := &api.ServerRequest{Duration: "10", NumClients: "1"}
	podData := &api.ClientRequest{DestinationIP: "localhost", Duration: "10", Timestamp: futureTime}
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
	err = client.Call("WorkerRPC.StartUDPServer", serpodData, &reply)
	// time.Sleep(2 * time.Second)
	err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	err = client.Call("WorkerRPC.StartUDPClient", podData, &reply)
	time.Sleep(15 * time.Second)
	client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	//HTTP test
	// // resultCh = make(chan string, 40)
	// err = client.Call("WorkerRPC.StartHTTPServer", podData, &reply)
	// // time.Sleep(2 * time.Second)
	// err = client.Call("WorkerRPC.StartHTTPClient", podData, &reply)
	// time.Sleep(15 * time.Second)
	// client.Call("WorkerRPC.Metrics", metricReq, &metricRes)
	klog.Info("TESTING COMPLETED!")
	////////////////////////////////////////////////
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
