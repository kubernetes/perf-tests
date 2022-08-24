/*
Copyright 2021 The Kubernetes Authors.

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

// Package network captures network performance metrics
// for protocol TCP,UDP,HTTP etc. The metrics are collected for baseline (1:1),
// scale (N:M) pod ratios.Client and server pods located on different worker
// nodes exchange traffic for specified time to measure the performance metrics.
package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	podReadyTimeout = 5 * time.Minute
	// initialDelay in seconds after which pod starts sending traffic.
	// Delay is used to synchronize all client pods to send traffic at same time.
	initialDelayForTestExecution = 15 * time.Second
	// networkPerformanceMetricsName indicates the measurement name
	networkPerformanceMetricsName = "NetworkPerformanceMetrics"
	netperfNamespace              = "netperf"
)

const (
	manifestPathPrefix                  = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/measurement/common/network/manifests"
	workerPodDeploymentManifestFilePath = manifestPathPrefix + "/" + "*deployment.yaml"
	networkTestRequestFilePath          = manifestPathPrefix + "/" + "networktestrequests.yaml"
	crdManifestFilePath                 = manifestPathPrefix + "/" + "*CustomResourceDefinition.yaml"
	clusterRoleBindingFilePath          = manifestPathPrefix + "/" + "roleBinding.yaml"
	customResourceDefinitionName        = "networktestrequests.clusterloader.io"
	rbacName                            = "networktestrequests-rbac"
)

var (
	crdGvk = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Kind:    "CustomResourceDefinition",
		Version: "v1",
	}

	rbacGvk = schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Kind:    "ClusterRoleBinding",
		Version: "v1",
	}

	crGvk = schema.GroupVersionKind{
		Group:   "clusterloader.io",
		Kind:    "NetworkTestRequest",
		Version: "v1alpha1",
	}
)

func init() {
	klog.Info("Registering Network Performance Measurement")
	if err := measurement.Register(networkPerformanceMetricsName, createNetworkPerformanceMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", networkPerformanceMetricsName, err)
	}
}

func createNetworkPerformanceMeasurement() measurement.Measurement {
	return &networkPerformanceMeasurement{}
}

type networkPerformanceMeasurement struct {
	k8sClient         kubernetes.Interface
	dynamicClient     dynamic.Interface
	resourceInterface dynamic.ResourceInterface
	framework         *framework.Framework

	numberOfServers int
	numberOfClients int
	podRatioType    string
	testDuration    time.Duration
	protocol        string

	// workerPodInfo stores list of podData for every worker node.
	podInfo    workerPodInfo
	metricLock sync.Mutex
	// metricVal stores MetricResponse received from worker node.
	metricVal [][]float64
	stopCh    chan struct{}
	// startTimeStampForTestExecution is a futureTimeStamp sent to all pods to start
	// sending the traffic at the same time.
	startTimeStampForTestExecution int64
}

func (npm *networkPerformanceMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		if err = npm.validate(config); err != nil {
			return nil, err
		}
		return nil, npm.start(config)
	case "gather":
		summary, err := npm.gather()
		if err != nil {
			return nil, err
		}
		return []measurement.Summary{summary}, err
	default:
		return nil, fmt.Errorf("unknown action: %v", action)
	}
}

func (npm *networkPerformanceMeasurement) start(config *measurement.Config) error {
	npm.initialize(config)
	if err := npm.prepareCluster(); err != nil {
		return err
	}
	if err := npm.createAndWaitForWorkerPods(); err != nil {
		return err
	}
	if err := npm.storeWorkerPods(); err != nil {
		return err
	}
	if err := npm.initializeInformer(); err != nil {
		return err
	}

	switch npm.podRatioType {
	case oneToOne, manyToMany:
		npm.execNToMTest()
	default:
		return fmt.Errorf("invalid Pod Ratio: %v", npm.podRatioType)
	}
	return nil
}

func (npm *networkPerformanceMeasurement) initialize(config *measurement.Config) {
	npm.k8sClient = config.ClusterFramework.GetClientSets().GetClient()
	npm.framework = config.ClusterFramework
	npm.dynamicClient = config.ClusterFramework.GetDynamicClients().GetClient()
	npm.podInfo.workerPodMap = make(map[string]podList)
}

func (npm *networkPerformanceMeasurement) initializeInformer() error {
	gvr, _ := meta.UnsafeGuessKindToResource(crGvk)
	npm.resourceInterface = npm.dynamicClient.Resource(gvr).Namespace(netperfNamespace)
	informer, err := getInformer(netperfNamespace, npm.dynamicClient, gvr)
	if err != nil {
		return fmt.Errorf("error getting informer:%s", err)
	}
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			npm.handleUpdateNetworkTestEvents(newObj)
		},
	})
	npm.stopCh = make(chan struct{})
	go informer.Run(npm.stopCh)
	return nil
}

func (npm *networkPerformanceMeasurement) prepareCluster() error {
	if err := client.CreateNamespace(npm.k8sClient, netperfNamespace); err != nil {
		return fmt.Errorf("error while creating namespace: %v", err)
	}
	if err := npm.framework.ApplyTemplatedManifests(clusterRoleBindingFilePath, nil); err != nil {
		return fmt.Errorf("error while creating clusterRoleBinding: %v", err)
	}
	if err := npm.framework.ApplyTemplatedManifests(crdManifestFilePath, nil); err != nil {
		return fmt.Errorf("error while creating CRD: %v", err)
	}
	return nil
}

func (npm *networkPerformanceMeasurement) cleanupCluster() {
	if npm.framework == nil {
		klog.V(1).Infof("Network measurement %s wasn't started, skipping the Dispose() step", npm)
		return
	}
	if err := npm.framework.DeleteObject(crdGvk, "", customResourceDefinitionName); err != nil {
		klog.Errorf("Failed to deleted CRD: %v", err)
	}
	if err := npm.framework.DeleteObject(rbacGvk, "", rbacName); err != nil {
		klog.Errorf("Failed to delete clusterRoleBinding: %v", err)
	}
	if err := client.DeleteNamespace(npm.k8sClient, netperfNamespace); err != nil {
		klog.Errorf("Failed to delete namespace: %v", err)
	}
	if err := client.WaitForDeleteNamespace(npm.k8sClient, netperfNamespace); err != nil {
		klog.Errorf("Waiting for namespace deletion failed: %v", err)
	}
}

func (npm *networkPerformanceMeasurement) createAndWaitForWorkerPods() error {
	// Create worker pods
	var replicas = map[string]interface{}{"Replicas": npm.numberOfClients + npm.numberOfServers}
	if err := npm.framework.ApplyTemplatedManifests(workerPodDeploymentManifestFilePath, replicas); err != nil {
		return fmt.Errorf("failed to create worked pods: %v ", err)
	}
	// Wait for all worker pods to be ready
	ctx, cancel := context.WithTimeout(context.TODO(), podReadyTimeout)
	defer cancel()
	selector := &util.ObjectSelector{Namespace: netperfNamespace}
	options := &measurementutil.WaitForPodOptions{
		DesiredPodCount:     func() int { return npm.numberOfClients + npm.numberOfServers },
		CallerName:          networkPerformanceMetricsName,
		WaitForPodsInterval: 2 * time.Second,
	}
	podStore, err := measurementutil.NewPodStore(npm.k8sClient, selector)
	if err != nil {
		return err
	}
	return measurementutil.WaitForPods(ctx, podStore, options)
}

func (*networkPerformanceMeasurement) String() string {
	return networkPerformanceMetricsName
}

func newWorkerPodData(podName, nodeName, podIP string) *workerPodData {
	return &workerPodData{podName: podName, workerNode: nodeName, podIP: podIP}
}

func (npm *networkPerformanceMeasurement) storeWorkerPods() error {
	pods, err := npm.k8sClient.CoreV1().Pods(netperfNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			klog.Errorf("IP address not found for %s", pod.Name)
			continue
		}
		npm.addPodWorker(newWorkerPodData(pod.Name, pod.Spec.NodeName, pod.Status.PodIP))
	}
	return nil
}

func (npm *networkPerformanceMeasurement) gather() (measurement.Summary, error) {
	npm.waitForMetricsFromPods()

	close(npm.stopCh)
	npm.cleanupCluster()

	resultSummary := npm.createResultSummary()
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version:   "v1",
		DataItems: resultSummary.DataItems,
	})
	if err != nil {
		klog.Infof("Failed to print metrics: %v", err)
	}
	summaryName := fmt.Sprintf(npm.String() + "_" + resultSummary.podRatio + "_" + resultSummary.protocol + "_" + resultSummary.service)
	return measurement.CreateSummary(summaryName, "json", content), nil
}

// Dispose disposes resources,objects after the measurement.
func (npm *networkPerformanceMeasurement) Dispose() {
	klog.Infof("Stopping network performance measurement...")
	npm.cleanupCluster()
}

func (npm *networkPerformanceMeasurement) handleUpdateNetworkTestEvents(newObj interface{}) {
	newRuntimeObj, ok := newObj.(runtime.Object)
	if newObj != nil && !ok {
		klog.Errorf("Unexpected object type: %v", newObj)
		return
	}
	// TODO(#1757): Stop relying on unstructured data.
	resourceContent, err := runtime.DefaultUnstructuredConverter.ToUnstructured(newRuntimeObj)
	if err != nil {
		klog.Errorf("Failed to convert to unstructured: %v", newObj)
		return
	}
	var metricResp metricResponse
	metricMap, ok := resourceContent["status"].(map[string]interface{})
	if !ok {
		klog.Errorf("Object doesn't have Status field: %v", newObj)
		return
	}
	if err = getMetricResponse(metricMap, &metricResp); err != nil {
		klog.Errorf("Failed to get metrics response: %v", err)
	}
	if len(metricResp.Metrics) == 0 && metricResp.Error != "" {
		klog.Errorf("Metrics error: %v", metricResp.Error)
	}
	npm.metricLock.Lock()
	defer npm.metricLock.Unlock()
	npm.metricVal = append(npm.metricVal, metricResp.Metrics)
}

func (npm *networkPerformanceMeasurement) execNToMTest() {
	podPairList := npm.podInfo.formUniquePodPair()
	npm.createCustomResourcePerUniquePodPair(podPairList)
}

func (npm *networkPerformanceMeasurement) createCustomResourcePerUniquePodPair(uniquePodPairList []podPair) {
	currTime := time.Now()
	npm.startTimeStampForTestExecution = currTime.Add(initialDelayForTestExecution).Unix()
	for pairIndex, podPair := range uniquePodPairList {
		templateMapping := npm.populateTemplate(podPair, pairIndex)
		if err := npm.framework.ApplyTemplatedManifests(networkTestRequestFilePath, templateMapping); err != nil {
			klog.Error(err)
		}
	}
}

func (npm *networkPerformanceMeasurement) addPodWorker(data *workerPodData) {
	if _, ok := npm.podInfo.workerPodMap[data.workerNode]; !ok {
		npm.podInfo.workerPodMap[data.workerNode] = []workerPodData{}
	}
	npm.podInfo.workerPodMap[data.workerNode] = append(npm.podInfo.workerPodMap[data.workerNode], *data)
}

func (npm *networkPerformanceMeasurement) calculateMetricDataValue(dataElem *measurementutil.DataItem, metricIndex int) {
	var aggregatePodPairMetrics []float64
	var metricResponse []float64

	npm.metricLock.Lock()
	defer npm.metricLock.Unlock()

	switch npm.podRatioType {
	case oneToOne:
		for _, metricResponse = range npm.metricVal {
			if len(metricResponse) > 0 {
				dataElem.Data[value] = metricResponse[metricIndex]
			} else {
				dataElem.Data[value] = 0
			}
		}
		klog.Info("Metric value: ", dataElem.Data[value])
	case manyToMany:
		for _, metricResponse = range npm.metricVal {
			if len(metricResponse) > 0 {
				// Sometimes iperf gives negative values for latency. As short-term fix
				// we are considering them as zero.
				if metricIndex == udpLatencyAverage && metricResponse[metricIndex] < 0 {
					aggregatePodPairMetrics = append(aggregatePodPairMetrics, 0)
					continue
				}
				aggregatePodPairMetrics = append(aggregatePodPairMetrics, metricResponse[metricIndex])
			}
		}
		percentile := getPercentile(aggregatePodPairMetrics)
		dataElem.Data[perc05] = percentile.percentile05
		dataElem.Data[perc50] = percentile.percentile50
		dataElem.Data[perc95] = percentile.percentile95
		klog.Info("Aggregate Metric value: ", aggregatePodPairMetrics)
	}
}

func (npm *networkPerformanceMeasurement) createResultSummary() testResultSummary {
	var resultSummary testResultSummary
	switch npm.protocol {
	case protocolTCP:
		npm.getMetricData(&resultSummary, tcpBandwidth, throughput)
		resultSummary.protocol = protocolTCP
	case protocolUDP:
		npm.getMetricData(&resultSummary, udpPacketPerSecond, packetPerSecond)
		npm.getMetricData(&resultSummary, udpJitter, jitter)
		npm.getMetricData(&resultSummary, udpLatencyAverage, latency)
		npm.getMetricData(&resultSummary, udpLostPacketsPercentage, lostPackets)
		resultSummary.protocol = protocolUDP
	case protocolHTTP:
		npm.getMetricData(&resultSummary, httpResponseTime, responseTime)
		resultSummary.protocol = protocolHTTP
	}
	resultSummary.service = "P2P"
	resultSummary.podRatio = npm.podRatioType
	return resultSummary
}

func (npm *networkPerformanceMeasurement) getMetricData(data *testResultSummary, metricIndex int, metricName string) {
	var metricDataItem measurementutil.DataItem
	metricDataItem.Data = make(map[string]float64)
	metricDataItem.Labels = make(map[string]string)
	metricDataItem.Labels["Metric"] = metricName
	npm.calculateMetricDataValue(&metricDataItem, metricIndex)
	metricDataItem.Unit = metricUnitMap[metricDataItem.Labels["Metric"]]
	data.DataItems = append(data.DataItems, metricDataItem)
	klog.V(3).Infof("TestResultSummary: %v", data)
}

func (npm *networkPerformanceMeasurement) waitForMetricsFromPods() {
	bufferTime := time.Duration(math.Max(10, float64(npm.numberOfClients)*0.1)) * time.Second
	timeout := initialDelayForTestExecution + npm.testDuration + bufferTime
	interval := 2 * time.Second
	if err := wait.Poll(interval, timeout, npm.checkResponseReceivedFromPods); err != nil {
		// TODO: Log pod names from which response is not received.
		klog.Errorf("Failed to receive response from %v pods", npm.numberOfClients-len(npm.metricVal))
	}
}

func (npm *networkPerformanceMeasurement) checkResponseReceivedFromPods() (bool, error) {
	npm.metricLock.Lock()
	defer npm.metricLock.Unlock()
	return len(npm.metricVal) == npm.numberOfClients, nil
}
