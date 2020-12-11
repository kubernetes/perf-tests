/*
Copyright 2020 The Kubernetes Authors.

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

// Package worker implements the worker related activities like starting TCP/UDP/HTTP client/server
// and collecting the metric output to be returned to the controller when requested.
package worker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"k8s.io/klog"
)

// Worker hold data required for facilitating measurement.
type Worker struct {
	stopCh       chan (struct{})
	parsedResult []float64
	err          error
	protocol     string
	workerDelay  time.Duration
	startTime    time.Time
}

// MetricResponse is the response sent back to controller after collecting measurement.
type MetricResponse struct {
	Result      []float64
	WorkerDelay time.Duration
}

// Status is the response sent back to controller when the request is unsuccessful.
type Status struct {
	Message string
}

type handlersMap map[string]func(http.ResponseWriter, *http.Request)

// http  listen ports.
const (
	workerListenPort = "5003"
	httpPort         = "5301"
)

var (
	// Command arguments for each protocol.This supports templates("{}"),
	// the value in template will be replaced by value in http request.
	// Iperf command args:
	// -c <destinationIP> : connect to destinationIP(as client).
	// -f K : report format KBytes/sec.
	// -l 20 : read/write buffer size 20 bytes.
	// -b 1M : bandwidth 1 Mbits/sec.
	// -i 1 : report stats every 1 sec.
	// -t duration : run  <duration> seconds.
	// -u : for udp measurement.
	// -e : enhanced reports, gives more metrics for udp.
	// -s : run in server mode.
	// -P numOfClients: handle <numOfClients> number of clients before disconnecting.
	udpClientArguments = []string{"-c", "{destinationIP}", "-u", "-f", "K", "-l", "20", "-b", "1M", "-e", "-i", "1", "-t", "{duration}"}
	udpServerArguments = []string{"-s", "-f", "K", "-u", "-e", "-i", "{duration}", "-P", "{numOfClients}"}
	tcpServerArguments = []string{"-s", "-f", "K", "-i", "{duration}", "-P", "{numOfClients}"}
	tcpClientArguments = []string{"-c", "{destinationIP}", "-f", "K", "-l", "20", "-b", "1M", "-i", "1", "-t", "{duration}"}
	// Siege command args:
	// -d1 : random delay between 0 to 1 sec.
	// -t<duration>S : run test for <duration> seconds.
	// -c1 : one concurrent user.
	httpClientArguments = []string{"http://" + "{destinationIP}" + ":" + httpPort + "/test", "-d1", "-t" + "{duration}" + "S", "-c1"}
)

func NewWorker() *Worker {
	return &Worker{}
}

// Start starts the worker.
func (w *Worker) Start() {
	w.stopCh = make(chan struct{})
	// TODO(#1631): make controller and worker communicate using k8s API (with CustomResources)
	// instead of http.
	w.listenToController()
}

func (w *Worker) listenToController() {
	handlers := handlersMap{
		"/startTCPServer":  w.StartTCPServer,
		"/startTCPClient":  w.StartTCPClient,
		"/startUDPServer":  w.StartUDPServer,
		"/startUDPClient":  w.StartUDPClient,
		"/startHTTPServer": w.StartHTTPServer,
		"/startHTTPClient": w.StartHTTPClient,
		"/metrics":         w.Metrics,
	}
	w.startListening(workerListenPort, handlers)
}

func (w *Worker) startListening(port string, handlers handlersMap) {
	for urlPath, handler := range handlers {
		http.HandleFunc(urlPath, handler)
	}
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		klog.Fatalf("Failed to start http server on port %v: %v", port, err)
	}
}

// Metrics returns the metrics collected.
func (w *Worker) Metrics(rw http.ResponseWriter, request *http.Request) {
	select {
	case <-w.stopCh:
		if w.err != nil {
			message := fmt.Sprintf("metrics collection failed: %v", w.err)
			w.sendResponse(rw, http.StatusInternalServerError, Status{Message: message})
			return
		}
		reply := MetricResponse{
			Result:      w.parsedResult,
			WorkerDelay: w.workerDelay,
		}
		w.sendResponse(rw, http.StatusOK, reply)
	default:
		w.sendResponse(rw, http.StatusInternalServerError, Status{Message: "metric collection in progress"})
	}
}

func (w *Worker) sendResponse(rw http.ResponseWriter, statusCode int, response interface{}) {
	rw.Header().Set("Content-Type", "application/json")
	marshalledResponse, err := json.Marshal(response)
	if err != nil {
		klog.Errorf("Error marshalling to json: %v", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	klog.V(3).Infof("Marshalled Response: %v", response)
	rw.WriteHeader(statusCode)
	if _, err := rw.Write(marshalledResponse); err != nil {
		klog.Errorf("Error writing response to ResponseWriter: %v", err)
	}
}

// StartHTTPServer starts an http server for http measurements.
func (w *Worker) StartHTTPServer(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting HTTP Server")
	go w.startListening(httpPort, handlersMap{"/test": w.Handler})
	w.sendResponse(rw, http.StatusOK, nil)
}

// StartTCPServer starts iperf server for tcp measurements.
func (w *Worker) StartTCPServer(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting TCP Server")
	w.startWork(rw, request, ProtocolTCP, "iperf", tcpServerArguments)
}

// StartUDPServer starts iperf server for udp measurements.
func (w *Worker) StartUDPServer(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting UDP Server")
	w.startWork(rw, request, ProtocolUDP, "iperf", udpServerArguments)
}

// StartHTTPClient starts an http client for http measurements.
func (w *Worker) StartHTTPClient(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting HTTP Client")
	w.startWork(rw, request, ProtocolHTTP, "siege", httpClientArguments)
}

// StartTCPClient starts iperf client for tcp measurements.
func (w *Worker) StartTCPClient(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting TCP Client")
	w.startWork(rw, request, ProtocolTCP, "iperf", tcpClientArguments)
}

// StartUDPClient starts iperf client for udp measurements.
func (w *Worker) StartUDPClient(rw http.ResponseWriter, request *http.Request) {
	klog.Info("Starting UDP Client")
	w.startWork(rw, request, ProtocolUDP, "iperf", udpClientArguments)
}

// Handler handles http requests for http measurements.
func (w *Worker) Handler(rw http.ResponseWriter, request *http.Request) {
	w.sendResponse(rw, http.StatusOK, "ok")
}

func (w *Worker) startWork(rw http.ResponseWriter, request *http.Request, protocol, command string, arguments []string) {
	arguments, err := populateTemplates(arguments, request)
	if err != nil {
		w.sendResponse(rw, http.StatusBadRequest, Status{Message: err.Error()})
		return
	}
	valMap, err := getValuesFromURL(request, []string{"timestamp", "duration"})
	if err != nil {
		w.sendResponse(rw, http.StatusBadRequest, Status{Message: err.Error()})
		return
	}
	timestamp, err := strconv.ParseInt(valMap["timestamp"], 10, 64)
	if err != nil {
		klog.Errorf("Invalid timestamp %v: %v", valMap["timestamp"], err)
	}
	go w.schedule(timestamp, valMap["duration"], protocol, command, arguments)
	w.sendResponse(rw, http.StatusOK, nil)
}

func (w *Worker) schedule(startTimestamp int64, duration string, protocol, command string, arguments []string) {
	defer close(w.stopCh)
	w.startTime = time.Unix(startTimestamp, 0)
	klog.Infof("About to wait until %v, current time: %v", w.startTime, time.Now())
	time.Sleep(w.startTime.Sub(time.Now()))
	w.workerDelay = time.Now().Sub(w.startTime)
	w.protocol = protocol
	result, err := w.executeCommand(command, arguments)
	if err != nil {
		klog.Errorf("Error executing command: %v", w.err)
		w.err = err
		return
	}
	parsedResult, err := parseResult(w.protocol, result, duration)
	if err != nil {
		klog.Errorf("Error parsing command response: %v", w.err)
		w.err = err
		return
	}
	w.parsedResult = parsedResult
}

func (w *Worker) executeCommand(commandString string, arguments []string) ([]string, error) {
	command := exec.Command(commandString, arguments...)
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errorOut, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	multiOut := io.MultiReader(out, errorOut)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return w.scanOutput(multiOut)
}

func (w *Worker) scanOutput(out io.Reader) ([]string, error) {
	var result []string
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		if line := scanner.Text(); len(line) > 0 {
			result = append(result, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
