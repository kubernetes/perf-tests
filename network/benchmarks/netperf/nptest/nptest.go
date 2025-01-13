/*
Copyright 2016 The Kubernetes Authors.

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
 nptest.go

 Dual-mode program - runs as both the orchestrator and as the worker nodes depending on command line flags
 The RPC API is contained wholly within this file.
*/

package main

// Imports only base Golang packages
import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type point struct {
	mss       int
	bandwidth string
	index     int
}

var mode string
var port string
var host string
var worker string
var kubenode string
var podname string
var testFrom, testTo int

var workerStateMap map[string]*workerState

var dataPoints map[string][]point
var dataPointKeys []string
var datapointsFlushed bool
var active_tests []*TestCase

type Result struct {
	Label  string          `json:"label"`
	Result json.RawMessage `json:"result"`
}

var results []Result

func addResult(label, resultJson string) {
	results = append(results, Result{
		Label:  label,
		Result: json.RawMessage(resultJson),
	})
}

var globalLock sync.Mutex

const (
	workerMode           = "worker"
	orchestratorMode     = "orchestrator"
	iperf3Path           = "/usr/local/bin/iperf3"
	qperfPath            = "/usr/local/bin/qperf"
	netperfPath          = "/usr/local/bin/netperf"
	netperfServerPath    = "/usr/local/bin/netserver"
	outputCaptureFile    = "/tmp/output.txt"
	jsonDataMarker       = "GENERATING JSON OUTPUT"
	jsonEndDataMarker    = "END JSON OUTPUT"
	mssMin               = 96
	mssMax               = 1460
	mssStepSize          = 64
	msgSizeMax           = 1 << 16
	msgSizeMin           = 1
	parallelStreams      = "8"
	rpcServicePort       = "5202"
	iperf3SctpPort       = "5004"
	localhostIPv4Address = "127.0.0.1"
)

// NetPerfRPC service that exposes RegisterClient and ReceiveOutput for clients
type NetPerfRPC int

// ClientRegistrationData stores a data about a single client
type ClientRegistrationData struct {
	Host     string
	KubeNode string
	Worker   string
	IP       string
}

// IperfClientWorkItem represents a single task for an Iperf client
type ClientWorkItem struct {
	Host   string
	Port   string
	Params TestParams
}

// IperfServerWorkItem represents a single task for an Iperf server
type ServerWorkItem struct {
	ListenPort string
	Timeout    int
}

// WorkItem represents a single task for a worker
type WorkItem struct {
	IsClientItem  bool
	IsServerItem  bool
	IsIdle        bool
	TestCaseIndex int
	ClientItem    ClientWorkItem
	ServerItem    ServerWorkItem
}

type workerState struct {
	sentServerItem bool
	idle           bool
	IP             string
	worker         string
}

// WorkerOutput stores the results from a single worker
type WorkerOutput struct {
	TestCaseIndex int
	Output        string
	Code          int
	Worker        string
	Type          TestType
}

func init() {
	flag.StringVar(&mode, "mode", "worker", "Mode for the daemon (worker | orchestrator)")
	flag.StringVar(&port, "port", rpcServicePort, "Port to listen on (defaults to 5202)")
	flag.IntVar(&testFrom, "testFrom", 0, "start from test number testFrom")
	flag.IntVar(&testTo, "testTo", 5, "end at test number testTo")

	workerStateMap = make(map[string]*workerState)
	results = make([]Result, 0)

	dataPoints = make(map[string][]point)
}

func initializeOutputFiles() {
	fd, err := os.OpenFile(outputCaptureFile, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println("Failed to open output capture file", err)
		os.Exit(2)
	}
	fd.Close()
}

func main() {
	initializeOutputFiles()
	flag.Parse()
	if !validateParams() {
		fmt.Println("Failed to parse cmdline args - fatal error - bailing out")
		os.Exit(1)
	}
	grabEnv()
	active_tests = make([]*TestCase, 0, testTo-testFrom)
	active_tests = append(active_tests, testcases[testFrom:testTo]...)
	fmt.Println("Running as", mode, "...")
	if mode == orchestratorMode {
		orchestrate()
	} else {
		startWork()
	}
	fmt.Println("Terminating npd")
}

func grabEnv() {
	worker = os.Getenv("worker")
	kubenode = os.Getenv("kubenode")
	podname = os.Getenv("HOSTNAME")
}

func validateParams() bool {
	if mode != workerMode && mode != orchestratorMode {
		fmt.Println("Invalid mode", mode)
		return false
	}

	if len(port) == 0 {
		fmt.Println("Invalid port", port)
		return false
	}

	if len(host) == 0 {
		host = os.Getenv("NETPERF_ORCH_SERVICE_HOST")
	}
	return true
}

func allWorkersIdle() bool {
	for _, v := range workerStateMap {
		if !v.idle {
			return false
		}
	}
	return true
}

func writeOutputFile(filename, data string) {
	fd, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println("Failed to append to existing file", filename, err)
		return
	}
	defer fd.Close()

	if _, err = fd.WriteString(data); err != nil {
		fmt.Println("Failed to append to existing file", filename, err)
	}
}

func registerDataPoint(label string, mss int, value string, index int) {
	if sl, ok := dataPoints[label]; !ok {
		dataPoints[label] = []point{{mss: mss, bandwidth: value, index: index}}
		dataPointKeys = append(dataPointKeys, label)
	} else {
		dataPoints[label] = append(sl, point{mss: mss, bandwidth: value, index: index})
	}
}

func flushDataPointsToCsv() {
	var buffer string

	// Write the MSS points for the X-axis before dumping all the testcase datapoints
	for _, points := range dataPoints {
		if len(points) == 1 {
			continue
		}
		buffer = fmt.Sprintf("%-45s, Maximum,", "MSS")
		for _, p := range points {
			buffer = buffer + fmt.Sprintf(" %d,", p.mss)
		}
		break
	}
	fmt.Println(buffer)

	for _, label := range dataPointKeys {
		buffer = fmt.Sprintf("%-45s,", label)
		points := dataPoints[label]
		var result float64
		for _, p := range points {
			fv, _ := strconv.ParseFloat(p.bandwidth, 64)
			if fv > result {
				result = fv
			}
		}
		buffer = buffer + fmt.Sprintf("%f,", result)
		for _, p := range points {
			buffer = buffer + fmt.Sprintf("%s,", p.bandwidth)
		}
		fmt.Println(buffer)
	}
	fmt.Println("END CSV DATA")
}

func flushResultJsonData() {
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Println("Error generating JSON:", err)
		return
	}

	fmt.Println(jsonDataMarker)
	fmt.Println(string(jsonData))
	fmt.Println(jsonEndDataMarker)
}

func serveRPCRequests(port string) {
	baseObject := new(NetPerfRPC)
	err := rpc.Register(baseObject)
	if err != nil {
		log.Fatal("failed to register rpc", err)
	}
	rpc.HandleHTTP()
	listener, e := net.Listen("tcp", ":"+port)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	err = http.Serve(listener, nil)
	if err != nil {
		log.Fatal("failed start server", err)
	}
}

// Blocking RPC server start - only runs on the orchestrator
func orchestrate() {
	serveRPCRequests(rpcServicePort)
}

// Walk the list of interfaces and find the first interface that has a valid IP
// Inside a container, there should be only one IP-enabled interface
func getMyIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return localhostIPv4Address
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}

func handleClientWorkItem(client *rpc.Client, workItem *WorkItem) {
	testCase := active_tests[workItem.TestCaseIndex]
	outputString := testCase.TestRunner(workItem.ClientItem)
	var reply int
	err := client.Call("NetPerfRPC.ReceiveOutput", WorkerOutput{Output: outputString, Worker: worker, Type: testCase.Type, TestCaseIndex: workItem.TestCaseIndex}, &reply)
	if err != nil {
		log.Fatal("failed to call client", err)
	}
	time.Sleep(10 * time.Second)
}

// isIPv6: Determines if an address is an IPv6 address
func isIPv6(address string) bool {
	x := net.ParseIP(address)
	return x != nil && x.To4() == nil && x.To16() != nil
}

// startWork : Entry point to the worker infinite loop
func startWork() {
	for {
		var client *rpc.Client
		var err error

		// Address recieved via command line
		address := host
		if isIPv6(address) {
			address = "[" + address + "]"
		}

		for {
			fmt.Println("Attempting to connect to orchestrator at", host)
			client, err = rpc.DialHTTP("tcp", address+":"+port)
			if err == nil {
				break
			}
			fmt.Println("RPC connection to ", host, " failed:", err)
			time.Sleep(5 * time.Second)
		}

		for {
			clientData := ClientRegistrationData{Host: podname, KubeNode: kubenode, Worker: worker, IP: getMyIP()}

			var workItem WorkItem

			if err := client.Call("NetPerfRPC.RegisterClient", clientData, &workItem); err != nil {
				// RPC server has probably gone away - attempt to reconnect
				fmt.Println("Error attempting RPC call", err)
				break
			}

			switch {
			case workItem.IsIdle:
				time.Sleep(5 * time.Second)
				continue

			case workItem.IsServerItem:
				fmt.Println("Orchestrator requests worker run iperf and netperf servers")
				go iperfServer()
				go qperfServer()
				go netperfServer()
				time.Sleep(1 * time.Second)

			case workItem.IsClientItem:
				handleClientWorkItem(client, &workItem)
			}
		}
	}
}

// Invoke and indefinitely run an iperf server
func iperfServer() {
	output, _ := cmdExec(iperf3Path, []string{iperf3Path, "-s", host, "-J", "-i", "60", "-D"}, 15)
	fmt.Println(output)
}

// Invoke and indefinitely run an qperf server
func qperfServer() {
	output, success := cmdExec(qperfPath, []string{qperfPath}, 15)
	if success {
		fmt.Println(output)
	}
}

// Invoke and indefinitely run netperf server
func netperfServer() {
	output, success := cmdExec(netperfServerPath, []string{netperfServerPath, "-D"}, 15)
	if success {
		fmt.Println(output)
	}
}

func cmdExec(command string, args []string, _ int32) (rv string, rc bool) {
	cmd := exec.Cmd{Path: command, Args: args}

	var stdoutput bytes.Buffer
	var stderror bytes.Buffer
	cmd.Stdout = &stdoutput
	cmd.Stderr = &stderror
	if err := cmd.Run(); err != nil {
		outputstr := stdoutput.String()
		errstr := stderror.String()
		fmt.Println("Failed to run", outputstr, "error:", errstr, err)
		return
	}

	rv = stdoutput.String()
	rc = true
	return
}

func (t *NetPerfRPC) ReceiveOutput(data *WorkerOutput, _ *int) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	fmt.Println("ReceiveOutput WorkItem TestCaseIndex: ", data.TestCaseIndex)
	testcase := active_tests[data.TestCaseIndex]

	outputLog := fmt.Sprintln("Received output from worker", data.Worker, "for test", testcase.Label,
		"from", testcase.SourceNode, "to", testcase.DestinationNode) + data.Output
	writeOutputFile(outputCaptureFile, outputLog)

	if testcase.BandwidthParser != nil {
		bw, mss := testcase.BandwidthParser(data.Output)
		registerDataPoint(testcase.Label, mss, fmt.Sprintf("%f", bw), data.TestCaseIndex)
		fmt.Println("Jobdone from worker", data.Worker, "Bandwidth was", bw, "Mbits/sec")
	}

	if testcase.JsonParser != nil {
		addResult(
			fmt.Sprintf("%s with MSS: %d", testcase.Label, testcase.MSS-mssStepSize),
			testcase.JsonParser(data.Output),
		)
		fmt.Println("Jobdone from worker", data.Worker, "JSON output generated")
	}

	return nil
}

func (t *NetPerfRPC) RegisterClient(data ClientRegistrationData, workItem *WorkItem) error {
	globalLock.Lock()
	defer globalLock.Unlock()

	state, ok := workerStateMap[data.Worker]

	if !ok {
		// For new clients, trigger an iperf server start immediately
		state = &workerState{sentServerItem: true, idle: true, IP: data.IP, worker: data.Worker}
		workerStateMap[data.Worker] = state
		workItem.IsServerItem = true
		workItem.ServerItem.ListenPort = "5201"
		workItem.ServerItem.Timeout = 3600
		return nil
	}

	// Worker defaults to idle unless the allocateWork routine below assigns an item
	state.idle = true

	// Give the worker a new work item or let it idle loop another 5 seconds
	allocateWorkToClient(state, workItem)
	return nil
}

func allocateWorkToClient(workerState *workerState, workItem *WorkItem) {
	if !allWorkersIdle() {
		workItem.IsIdle = true
		return
	}

	// System is all idle - pick up next work item to allocate to client
	for n, v := range active_tests {
		if v.Finished {
			continue
		}
		if v.SourceNode != workerState.worker {
			workItem.IsIdle = true
			return
		}
		if _, ok := workerStateMap[v.DestinationNode]; !ok {
			workItem.IsIdle = true
			return
		}
		fmt.Printf("Requesting jobrun '%s' from %s to %s for MSS %d for MsgSize %d\n", v.Label, v.SourceNode, v.DestinationNode, v.MSS, v.MsgSize)
		workItem.IsClientItem = true
		workItem.TestCaseIndex = n
		workerState.idle = false

		if !v.ClusterIP {
			workItem.ClientItem.Host = workerStateMap[workerState.worker].IP
		} else {
			workItem.ClientItem.Host = os.Getenv("NETPERF_W2_SERVICE_HOST")
		}

		workItem.ClientItem.Params = v.TestParams

		if v.MSS != 0 && v.MSS < mssMax {
			v.MSS += mssStepSize
		} else {
			v.Finished = true
		}

		if v.Type == netperfTest {
			workItem.ClientItem.Port = "12865"
		} else {
			workItem.ClientItem.Port = "5201"
		}

		return
	}

	for _, v := range active_tests {
		if !v.Finished {
			return
		}
	}

	if !datapointsFlushed {
		fmt.Println("ALL TESTCASES AND MSS RANGES COMPLETE - GENERATING CSV OUTPUT")
		flushDataPointsToCsv()
		flushResultJsonData()
		datapointsFlushed = true
	}

	workItem.IsIdle = true
}
