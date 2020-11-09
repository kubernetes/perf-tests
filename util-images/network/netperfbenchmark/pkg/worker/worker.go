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

/*
Package worker implements the worker related activities like starting TCP/UDP/HTTP client/server
and collecting the metric output to be returned to the controller when requested.
*/
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
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog"
)

var listener net.Listener
var resultStatus = make(chan string)
var result []string
var tstDuration string
var tstProtocol string
var startedAt int64
var futureTime int64
var iperfUDPFn = []string{"", "", "", "Sum", "Sum", "Sum", "Sum", "Sum", "Avg", "Avg", "Min", "Max", "Avg", "Sum"}
var iperfTCPFn = []string{"Sum", "Avg"}

const tcpMtrCnt = 2
const udpMtrCnt = 11
const httpMetrCnt = 12

//StartResponse is the response sent back to controller on starting a measurement
type startWrkResponse struct {
	error string
}

//MetricResponse is the response sent back to controller after collecting measurement
type metricResponse struct {
	Result          []float64
	WorkerStartTime string
	Error           string
}

//http  listen ports
const (
	workerListenPort = "5003"
	httpPort         = "5301"
)

//Protocols supported
const (
	protocolTCP  = "TCP"
	protocolUDP  = "UDP"
	protocolHTTP = "HTTP"
)

//Start worker
func Start() {
	listenToCntrlr()
}

func listenToCntrlr() {
	h := map[string]func(http.ResponseWriter, *http.Request){"/startTCPServer": StartTCPServer,
		"/startTCPClient": StartTCPClient, "/startUDPServer": StartUDPServer, "/startUDPClient": StartUDPClient,
		"/startHTTPServer": StartHTTPServer, "/startHTTPClient": StartHTTPClient, "/metrics": Metrics}
	startListening(workerListenPort, h)
	klog.Info("Started listening to Server")
}

func startListening(port string, handlers map[string]func(http.ResponseWriter, *http.Request)) {
	klog.Info("In StartHTTPServer")
	for p, h := range handlers {
		http.HandleFunc(p, h)
	}
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		klog.Fatalf("Failed starting http server for port: %v, Error: %v", port, err)
	}
}

//Metrics returns the metrics collected
func Metrics(res http.ResponseWriter, req *http.Request) {
	var reply metricResponse
	klog.Info("In metrics")
	var stat string
	select {
	case stat = <-resultStatus:
		if stat != "OK" {
			klog.Error("Error collecting metrics:", stat)
			reply.Error = "metrics collection failed:" + stat
			createResp(reply, &res)
			return
		}
		klog.Info("Metrics collected")
	default:
		klog.Info("Metric collection in progress")
		reply.Error = "metric collection in progress"
		createResp(reply, &res)
		return
	}
	o, err := parseResult()
	if err != nil {
		reply.Error = err.Error()
		createResp(reply, &res)
		return
	}
	reply.Result = o
	//startime and other variables below can be removed in future, for debugging during initial runs
	stStr := strconv.FormatInt(startedAt, 10)
	ftStr := strconv.FormatInt(futureTime, 10)
	dtStr := strconv.FormatInt(futureTime-startedAt, 10)
	reply.WorkerStartTime = "StartedAt:" + stStr + " FutureTime:" + ftStr + " diffTime:" + dtStr
	createResp(reply, &res)
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

//StartTCPClient starts iperf client for tcp measurements
func StartTCPClient(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartTCPClient")
	klog.Info("Req:", req)
	ts, dur, destIP, _ := parseURLParam(req)
	if dur == "" || destIP == "" {
		createResp(startWrkResponse{error: "missing/invalid required parameters"}, &res)
		return
	}
	go schedule(protocolTCP, ts, dur,
		"iperf", []string{"-c", destIP, "-f", "K", "-l",
			"20", "-b", "1M", "-i", "1", "-t", dur})
	createResp(startWrkResponse{}, &res)
}

//StartTCPServer starts iperf server for tcp measurements
func StartTCPServer(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartTCPServer")
	klog.Info("Req:", req)
	ts, dur, _, numcl := parseURLParam(req)
	if dur == "" || numcl == "" {
		createResp(startWrkResponse{error: "missing/invalid required parameters"}, &res)
		return
	}
	go schedule(protocolTCP, ts, dur, "iperf", []string{"-s", "-f", "K", "-i", dur, "-P", numcl})
	createResp(startWrkResponse{}, &res)
}

//StartUDPServer starts iperf server for udp measurements
func StartUDPServer(res http.ResponseWriter, req *http.Request) {
	//iperf -s -u -e -i <duration> -P <num parallel clients>
	klog.Info("In StartUDPServer")
	ts, dur, _, numcl := parseURLParam(req)
	if dur == "" || numcl == "" {
		createResp(startWrkResponse{error: "missing/invalid required parameters"}, &res)
		return
	}
	go schedule(protocolUDP, ts, dur, "iperf", []string{"-s", "-f", "K", "-u", "-e", "-i", dur, "-P", numcl})
	createResp(startWrkResponse{}, &res)
}

//StartUDPClient starts iperf client for udp measurements
func StartUDPClient(res http.ResponseWriter, req *http.Request) {
	//iperf -c localhost -u -l 20 -b 1M -e -i 1
	klog.Info("In StartUDPClient")
	ts, dur, destIP, _ := parseURLParam(req)
	if dur == "" || destIP == "" {
		createResp(startWrkResponse{error: "missing/invalid required parameters"}, &res)
		return
	}
	go schedule(protocolUDP, ts, dur, "iperf", []string{"-c", destIP, "-u", "-f", "K", "-l", "20", "-b", "1M", "-e", "-i", "1", "-t", dur})
	createResp(startWrkResponse{}, &res)
}

//StartHTTPServer starts an http server for http measurements
func StartHTTPServer(res http.ResponseWriter, req *http.Request) {
	klog.Info("In StartHTTPServer")
	go startListening(httpPort, map[string]func(http.ResponseWriter, *http.Request){"/test": Handler})
	createResp(startWrkResponse{}, &res)
}

//StartHTTPClient starts an siege client for http measurements
func StartHTTPClient(res http.ResponseWriter, req *http.Request) {
	//// siege http://localhost:5301/test -d1 -r1 -c1 -t10S
	//c concurrent r repetitions t time d delay in sec between 1 and d
	klog.Info("In StartHTTPClient")
	ts, dur, destIP, _ := parseURLParam(req)
	if dur == "" || destIP == "" {
		createResp(startWrkResponse{error: "missing/invalid required parameters"}, &res)
		return
	}
	go schedule(protocolHTTP, ts, dur, "siege",
		[]string{"http://" + destIP + ":" + httpPort + "/test",
			"-d1", "-t" + dur + "S", "-c1"})
	createResp(startWrkResponse{}, &res)
}

//Handler handles http requests for http measurements
func Handler(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(res, "hi\n")
}

func parseURLParam(req *http.Request) (int64, string, string, string) {
	values := req.URL.Query()
	ts := values.Get("timestamp")
	dur := values.Get("duration")
	dstIP := values.Get("destIP")
	numcl := values.Get("numCls")
	var tsint int64
	var err error
	if ts != "" {
		tsint, err = strconv.ParseInt(ts, 10, 64)
		if err != nil {
			klog.Info("Invalid timestamp:", ts, " ", err)
		}
	}
	return tsint, dur, dstIP, numcl

}

func schedule(protocol string, futureTimestamp int64, duration string, command string, args []string) {
	//If future time is in past,run immediately
	klog.Info("About to wait for futuretime:", futureTimestamp)
	klog.Info("Current time:", time.Now().Unix())
	time.Sleep(time.Duration(futureTimestamp-time.Now().Unix()) * time.Second)
	startedAt = time.Now().Unix()
	futureTime = futureTimestamp
	tstDuration = duration
	tstProtocol = protocol
	execCmd(duration, command, args)
}

func execCmd(duration string, command string, args []string) {
	cmd := exec.Command(command, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		klog.Error("unable to obtain Stdout:", err)
		resultStatus <- err.Error()
		return
	}
	eout, err := cmd.StderrPipe()
	if err != nil {
		klog.Error("unable to obtain Stderr:", err)
		resultStatus <- err.Error()
		return
	}
	multiRdr := io.MultiReader(out, eout)
	err = cmd.Start()
	if err != nil {
		resultStatus <- err.Error()
	}
	scanOutput(&multiRdr)
}

func scanOutput(out *io.Reader) {
	scanner := bufio.NewScanner(*out)
	klog.Info("Starting scan for output")
	for scanner.Scan() {
		l := scanner.Text()
		// klog.Info(line)
		if l == "" {
			continue
		}
		result = append(result, l)
	}
	klog.Info("Command executed,sending result back")
	if err := scanner.Err(); err != nil {
		klog.Error("Error", err)
		resultStatus <- err.Error()
		return
	}
	resultStatus <- "OK"
}

func parseResult() ([]float64, error) {
	klog.Info("Parsing", result[0])
	switch tstProtocol {
	case protocolTCP:
		return parseTCP(result), nil
	case protocolUDP:
		return parseUDP(result), nil
	case protocolHTTP:
		return parseHTTP(result), nil
	default:
		return nil, errors.New("invalid protocol:" + result[0])

	}
}

func parseTCP(result []string) []float64 {
	klog.Info("In parseTCP")
	sumResult := make([]float64, 0, tcpMtrCnt)
	unitReg := regexp.MustCompile(`\[\s+|\]\s+|KBytes\s+|KBytes/sec\s*|sec\s+|ms\s+|us\s*`)
	mulSpaceReg := regexp.MustCompile(`\s+`)
	hypSpcReg := regexp.MustCompile(`\-\s+`)
	cnt := 0
	sessionID := make(map[string]bool)
	for _, op := range result {
		klog.Info(op)
		if !strings.Contains(op, "0.0-"+tstDuration+".0") { //single digit dur has probs
			continue
		}
		frmtString := hypSpcReg.ReplaceAllString(mulSpaceReg.ReplaceAllString(unitReg.ReplaceAllString(op, " "), " "), "-")
		klog.Info("Trim info:", frmtString)
		split := strings.Split(frmtString, " ")
		//for bug in iperf tcp
		//if the record is for the complete duration of run
		if len(split) >= 3 && "SUM" != split[1] && split[2] == "0.0-"+tstDuration+".0" {
			if _, ok := sessionID[split[1]]; ok {
				continue
			}
			for i, v := range split {
				klog.Info("Split", i, ":", v)
				if i == 1 {
					sessionID[v] = true
					continue
				}
				//first index and hte last is ""
				if i == 0 || i == 2 || i == 5 {
					continue
				}
				tmp, err := strconv.ParseFloat(v, 64)
				if err != nil {
					klog.Error("Conversion error", err)
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
	klog.Info("Final output:", sumResult)
	return sumResult
}

func parseUDP(result []string) []float64 {
	klog.Info("In parseUDP")
	sumResult := make([]float64, 0, udpMtrCnt)
	unitReg := regexp.MustCompile(`%|\[\s+|\]\s+|KBytes\s+|KBytes/sec\s+|sec\s+|pps\s*|ms\s+|/|\(|\)\s+`)
	mulSpaceReg := regexp.MustCompile(`\s+`)
	hypSpcReg := regexp.MustCompile(`\-\s+`)
	cnt := 0
	for _, op := range result {
		if !strings.Contains(op, "0.00-"+tstDuration+".00") {
			continue
		}
		frmtString := hypSpcReg.ReplaceAllString(mulSpaceReg.ReplaceAllString(unitReg.ReplaceAllString(op, " "), " "), "-")
		klog.Info("Trim info:", frmtString)
		split := strings.Split(frmtString, " ")
		//if the record is for the complete duration of run
		if len(split) >= 13 && "SUM" != split[1] && split[2] == "0.00-"+tstDuration+".00" {
			for i, v := range split {
				klog.Info("Split", i, ":", v)
				//first index and hte last is ""
				if i == 0 || i == 1 || i == 2 || i == 14 {
					continue
				}
				tmp, err := strconv.ParseFloat(v, 64)
				if err != nil {
					klog.Error("Conversion error", err)
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
	klog.Info("Final output:", sumResult)
	return sumResult
}

func parseHTTP(result []string) []float64 {
	canAppend := false
	sumResult := make([]float64, 0, httpMetrCnt)
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
