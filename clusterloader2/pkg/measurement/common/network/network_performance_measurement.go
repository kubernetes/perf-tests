package network

import (
	"context"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func init() {
	klog.Info("Registering Network Measurement")
	if err := measurement.Register(networkPerfMetricsName, createNetworkPerfMetricsMeasurement); err != nil {
		klog.Fatal("Cannot register %s: %v", networkPerfMetricsName, err)
	}
}

func createNetworkPerfMetricsMeasurement() measurement.Measurement {
	return &networkPerfMetricsMeasurement{}
}

type networkPerfMetricsMeasurement struct {
	k8sClient       kubernetes.Interface
	framework       *framework.Framework
	podReplicas     int
	namespace       string
	podRatio        string
	testDuration    int
	protocol        string
	templateMapping map[string]interface{}
	startTime       time.Time
}

func (npm *networkPerfMetricsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {

	klog.Info("In network execute")
	action, err := npm.validate(config)
	klog.Info("In network execute:action:", action)
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		npm.start(config)
	case "gather":
		summary, err := npm.gather(config)
		if err != nil && !errors.IsMetricViolationError(err) {
			klog.Error("Error in metrics:", err)
			return nil, err
		}
		klog.Info("metric:", summary)
		return []measurement.Summary{summary}, err
	default:
	}

	return nil, nil
}

func (npm *networkPerfMetricsMeasurement) Dispose() {

}

func (npm *networkPerfMetricsMeasurement) start(config *measurement.Config) error {
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()

	Start(k8sClient, netperfNamespace)
	if err := npm.initialize(config); err != nil {
		return err
	}

	//create namespace for the worker-pods
	npm.k8sClient = k8sClient
	npm.namespace = netperfNamespace
	if err := client.CreateNamespace(k8sClient, netperfNamespace); err != nil {
		klog.Info("Error starting measurement:", err)
	}

	//Create worker pods using manifest files
	if err := npm.createWorkerPods(); err != nil {
		return err
	}

	//wait for specified num of worker pods to be ready
	if err := npm.waitForWorkerPodsReady(); err != nil {
		return err
	}

	npm.storeWorkerPods()

	ExecuteTest(npm.podRatio, npm.testDuration, npm.protocol)

	return nil
}

func (npm *networkPerfMetricsMeasurement) initialize(config *measurement.Config) error {
	podReplicas, err := util.GetInt(config.Params, "podReplicas")
	if err != nil {
		return err
	}
	klog.Info("podReplicas:", podReplicas)
	npm.framework = config.ClusterFramework
	npm.podReplicas = podReplicas
	npm.templateMapping = map[string]interface{}{"Replicas": podReplicas}
	return nil
}

func (npm *networkPerfMetricsMeasurement) createWorkerPods() error {
	klog.Info("createWorkerPods:", manifestsPathPrefix)
	return npm.framework.ApplyTemplatedManifests(manifestsPathPrefix, npm.templateMapping)
}

func (npm *networkPerfMetricsMeasurement) waitForWorkerPodsReady() error {
	var podNum = npm.podReplicas
	var weightedPodTReadyTimeout = podNum * 1
	var checkWorkerPodReadyTimeout = time.Duration(weightedPodTReadyTimeout) * time.Second
	klog.Info("waitForWorkerPodsReady:", podNum, weightedPodTReadyTimeout, checkWorkerPodReadyTimeout)
	return wait.Poll(checkWorkerPodReadyInterval, checkWorkerPodReadyTimeout, npm.checkWorkerPodsReady)
}

func (npm *networkPerfMetricsMeasurement) checkWorkerPodsReady() (bool, error) {
	options := metav1.ListOptions{LabelSelector: workerLabel}
	pods, err := npm.k8sClient.CoreV1().Pods(npm.namespace).List(context.TODO(), options)
	if len(pods.Items) == npm.podReplicas {
		return true, err
	}
	return false, err
}

func (*networkPerfMetricsMeasurement) String() string {
	return networkPerfMetricsName
}

func (npm *networkPerfMetricsMeasurement) storeWorkerPods() {
	options := metav1.ListOptions{LabelSelector: workerLabel}
	pods, _ := npm.k8sClient.CoreV1().Pods(npm.namespace).List(context.TODO(), options)

	for _, pod := range pods.Items {
		podData := &WorkerPodData{PodName: pod.Name, PodIp: pod.Status.PodIP, WorkerNode: pod.Spec.NodeName}
		klog.Info("PodData :", *podData)
		populateWorkerPodList(podData)
	}

}

func (m *networkPerfMetricsMeasurement) gather(config *measurement.Config) (measurement.Summary, error) {
	dat := GetMetricsForDisp()
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version: "v1",
		// DataItems: []measurementutil.DataItem{latency.ToPerfData(p.String())}
		DataItems: dat.DataItems,
	})
	if err != nil {
		klog.Info("Pretty Print to Json Err:", err)
	}
	return measurement.CreateSummary(m.String()+dat.Client_Server_Ratio+dat.Protocol+dat.Service, "json", content), nil
}

func (npm *networkPerfMetricsMeasurement) validate(config *measurement.Config) (string, error) {
	var ratio, protocol string
	var duration int
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return "", err
	}

	if duration, err = util.GetInt(config.Params, "duration"); err != nil {
		return action, err
	}

	if ratio, err = util.GetString(config.Params, "ratio"); err != nil {
		return action, err
	}
	if protocol, err = util.GetString(config.Params, "protocol"); err != nil {
		return action, err
	}

	if protocol != Protocol_TCP && protocol != Protocol_UDP && protocol != Protocol_HTTP {
		return action, err
	}

	npm.testDuration = duration
	npm.podRatio = ratio
	npm.protocol = protocol
	return action, nil
}
