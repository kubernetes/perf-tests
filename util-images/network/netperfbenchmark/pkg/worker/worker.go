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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// Worker holds data required for facilitating measurement.
type Worker struct {
	resourceInterface dynamic.ResourceInterface
	podName           string
	work              work
	stopCh            chan struct{}
}

type work struct {
	resourceName string
	workType     string
	resourceSpec resourceProperties
	arguments    []string
}

type resourceProperties struct {
	ServerPodIP          string
	ServerPodName        string
	Duration             int64
	NumberOfClients      int64
	Protocol             string
	ClientPodIP          string
	ClientPodName        string
	ClientStartTimestamp int64
}

type handlersMap map[string]func(http.ResponseWriter, *http.Request)

// http server listen port.
const (
	httpPort  = "5301"
	namespace = "netperf"
)

var (
	protocolCommandMap = map[string]string{
		ProtocolHTTP: "siege",
		ProtocolTCP:  "iperf",
		ProtocolUDP:  "iperf",
	}

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
	udpClientArguments = []string{"-c", "{serverPodIP}", "-u", "-f", "K", "-e", "-i", "1", "-t", "{duration}"}
	udpServerArguments = []string{"-s", "-f", "K", "-u", "-e", "-i", "{duration}", "-P", "{numberOfClients}"}
	tcpServerArguments = []string{"-s", "-f", "K", "-i", "{duration}", "-P", "{numberOfClients}"}
	tcpClientArguments = []string{"-c", "{serverPodIP}", "-f", "K", "-i", "1", "-t", "{duration}"}
	// Siege command args:
	// -d1 : random delay between 0 to 1 sec.
	// -t<duration>S : run test for <duration> seconds.
	// -c1 : one concurrent user.
	httpClientArguments = []string{"http://" + "{serverPodIP}" + ":" + httpPort + "/test", "-d1", "-t" + "{duration}" + "S", "-c1"}

	protocolArgumentMap = map[string]map[string][]string{
		"client": {
			ProtocolHTTP: httpClientArguments,
			ProtocolTCP:  tcpClientArguments,
			ProtocolUDP:  udpClientArguments,
		},
		"server": {
			ProtocolTCP: tcpServerArguments,
			ProtocolUDP: udpServerArguments,
		},
	}
)

var gvk = schema.GroupVersionKind{Group: "clusterloader.io", Kind: "NetworkTestRequest", Version: "v1alpha1"}

func NewWorker() *Worker {
	return &Worker{}
}

// Start starts the worker.
func (w *Worker) Start(extraArguments map[string]*string) {
	w.populatePodName()
	w.initialize(extraArguments)
}

func (w *Worker) initialize(extraArguments map[string]*string) {
	w.addExtraArguments(extraArguments)
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	k8sClient, err := getDynamicClient()
	if err != nil {
		klog.Fatalf("Error getting dynamic client:%s", err)
	}
	w.resourceInterface = k8sClient.Resource(gvr).Namespace(namespace)
	informer, err := getInformer(w.getCustomResourceLabelSelector(), namespace, k8sClient, gvr)
	if err != nil {
		klog.Fatalf("Error getting informer:%s", err)
	}
	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			w.handleCustomResource(obj)
		},
	})
	if err != nil {
		klog.Fatalf("Error registering event handlers: %v", err)
	}
	w.stopCh = make(chan struct{})
	informer.Run(w.stopCh)
}

func (w *Worker) addExtraArguments(extraAguments map[string]*string) {
	for argumentName, argumentValue := range extraAguments {
		if *argumentValue == "" {
			continue
		}
		split := strings.Split(argumentName, "_")
		if len(split) < 2 {
			klog.Errorf("Extra argument name should be in format <type_protocol>: %s", argumentName)
			return
		}
		protocolArgumentMap[split[0]][split[1]] = append(protocolArgumentMap[split[0]][split[1]], *argumentValue)
	}
}

func (w *Worker) populatePodName() {
	var isPodNameAvailable bool
	w.podName, isPodNameAvailable = os.LookupEnv("POD_NAME")
	if !isPodNameAvailable {
		klog.Fatal(errors.New("pod name not set as environment variable"))
	}
	klog.Info("Pod Name set:", w.podName)
}

func (w *Worker) getCustomResourceLabelSelector() string {
	return fmt.Sprintf("%s in (clientPodName,serverPodName)", w.podName)
}

func (w *Worker) handleCustomResource(obj interface{}) {
	newRuntimeObj, ok := obj.(runtime.Object)
	if obj != nil && !ok {
		klog.Errorf("Error casting object: %s", obj)
		return
	}
	err := w.populateResourceSpec(newRuntimeObj)
	if err != nil {
		w.handleError(fmt.Errorf("populating resource spec failed: %v", err))
		return
	}
	klog.Info("Recevied add event for resource with spec:", w.work.resourceSpec)
	switch w.podName {
	case w.work.resourceSpec.ClientPodName:
		w.work.workType = "client"
	case w.work.resourceSpec.ServerPodName:
		w.work.workType = "server"
	default:
		w.handleError(errors.New("pod name not set as client or server"))
		return
	}
	w.startWork()
}

func (w *Worker) populateResourceSpec(object runtime.Object) error {
	resourceContent, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return fmt.Errorf("error converting event Object to unstructured.Event object: %s", object)
	}
	metadata := resourceContent["metadata"].(map[string]interface{})
	w.work.resourceName = metadata["name"].(string)
	resourceSpecMap := resourceContent["spec"].(map[string]interface{})
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resourceSpecMap, &w.work.resourceSpec); err != nil {
		return fmt.Errorf("error converting custom resource properties: %s", err)
	}
	return nil
}

func (w *Worker) startWork() {
	if w.work.resourceSpec.Protocol == ProtocolHTTP && w.work.workType == "server" {
		w.StartHTTPServer()
		return
	}
	var err error
	properties := extractParameters(w.work.resourceSpec)
	w.work.arguments, err = populateTemplates(protocolArgumentMap[w.work.workType][w.work.resourceSpec.Protocol], properties)
	if err != nil {
		w.handleError(fmt.Errorf("populating template failed: %v", err))
		return
	}
	klog.Infof("Populated templates: %s", w.work.arguments)
	startTimestamp := w.getStartTimestamp()
	w.schedule(startTimestamp)
}

func (w *Worker) getStartTimestamp() int64 {
	if w.work.workType == "client" {
		return w.work.resourceSpec.ClientStartTimestamp
	}
	return time.Now().Unix()
}

func (w *Worker) updateStatus(status map[string]interface{}) error {
	resource, err := w.resourceInterface.Get(context.TODO(), w.work.resourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("updating status failed: %v", err)
	}
	resourceContent := resource.UnstructuredContent()
	resourceContent["status"] = status
	var unstructuredRes unstructured.Unstructured
	unstructuredRes.SetUnstructuredContent(resourceContent)
	// TODO(@VivekThrivikraman-est): add retries.
	_, err = w.resourceInterface.Update(context.TODO(), &unstructuredRes, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}
	klog.Info("Status Updated")
	return nil
}

func (w *Worker) handleError(err error) {
	klog.Error(err)
	if err := w.updateStatus(makeErrorStatus(err)); err != nil {
		klog.Error(err)
	}
}

func (w *Worker) schedule(startTimestamp int64) {
	startTime := time.Unix(startTimestamp, 0)
	klog.Infof("About to wait until %v, current time: %v", startTime, time.Now())
	time.Sleep(startTime.Sub(time.Now()))
	workerDelay := time.Now().Sub(startTime)
	command := protocolCommandMap[w.work.resourceSpec.Protocol]
	result, err := executeCommand(command, w.work.arguments)
	if err != nil {
		w.handleError(fmt.Errorf("error executing command %v %v: %v", command, w.work.arguments, err))
		return
	}
	if !w.shouldParseResponse() {
		return
	}
	parsedResult, err := parseResult(w.work.resourceSpec.Protocol, result)
	if err != nil {
		w.handleError(fmt.Errorf("error parsing command response: %v", err))
		return
	}
	klog.Info("Parsed Response:", parsedResult)
	if err := w.updateStatus(makeSuccessStatus(parsedResult, workerDelay.Seconds())); err != nil {
		klog.Error(err)
	}

}

func (w *Worker) shouldParseResponse() bool {
	return (w.work.resourceSpec.Protocol != ProtocolHTTP && w.work.workType == "server") ||
		(w.work.resourceSpec.Protocol == ProtocolHTTP && w.work.workType == "client")
}

func (w *Worker) startListening(port string, handlers handlersMap) {
	for urlPath, handler := range handlers {
		http.HandleFunc(urlPath, handler)
	}
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		klog.Fatalf("Failed to start http server on port %v: %v", port, err)
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
func (w *Worker) StartHTTPServer() {
	klog.Info("Starting HTTP Server")
	go w.startListening(httpPort, handlersMap{"/test": w.Handler})
}

// Handler handles http requests for http measurements.
func (w *Worker) Handler(rw http.ResponseWriter, _ *http.Request) {
	w.sendResponse(rw, http.StatusOK, "ok")
}
