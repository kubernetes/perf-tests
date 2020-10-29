package network

import (
	"context"
	"time"

	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"

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
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		klog.Info("Error starting action:", err)
		return nil, err
	}
	klog.Info("In network execute:action:", action)

	switch action {
	case "start":
		err = npm.validate(config)
		if err != nil {
			klog.Info("Error starting action:", err)
		}
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

func (npm *networkPerfMetricsMeasurement) start(config *measurement.Config) error {
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()

	Start(k8sClient)
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
		klog.Info("ERROR waiting:", err)
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
	workerPodReadyInterval, weightedPodReadyTimeout := npm.getWeightedTimerValuesForPoll()
	return wait.Poll(workerPodReadyInterval, weightedPodReadyTimeout, npm.checkWorkerPodsReady)
}

func (npm *networkPerfMetricsMeasurement) checkWorkerPodsReady() (bool, error) {
	// options := metav1.ListOptions{LabelSelector: workerLabel}
	options := metav1.ListOptions{}
	pods, err := npm.k8sClient.CoreV1().Pods(npm.namespace).List(context.TODO(), options)
	klog.Info("POLL pods:", len(pods.Items), " namespace:", npm.namespace, " Options:", options)
	var podsWithIps = 0
	if len(pods.Items) == npm.podReplicas {
		for _, pod := range pods.Items {
			if pod.Status.PodIP != "" {
				podsWithIps++
			}
		}
		if podsWithIps == len(pods.Items) {
			return true, err
		}
	}
	return false, err
}

func (npm *networkPerfMetricsMeasurement) getWeightedTimerValuesForPoll() (time.Duration, time.Duration) {
	var weightedPodReadyTimeout = npm.podReplicas * 3
	podReadyTimeout := time.Duration(weightedPodReadyTimeout) * time.Second

	var workerPodReadyInterval time.Duration
	workerPodReadyInterval = time.Duration(2) * time.Second

	klog.Info("waitForWorkerPodsReady:  , podReadyTimeout: ", workerPodReadyInterval, podReadyTimeout)
	return workerPodReadyInterval, podReadyTimeout

}

func (*networkPerfMetricsMeasurement) String() string {
	return networkPerfMetricsName
}

func (npm *networkPerfMetricsMeasurement) storeWorkerPods() {
	// options := metav1.ListOptions{LabelSelector: workerLabel}
	time.Sleep(5 * time.Second)
	options := metav1.ListOptions{}
	pods, _ := npm.k8sClient.CoreV1().Pods(npm.namespace).List(context.TODO(), options)
	klog.Info("populating pods:", len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			podData := &WorkerPodData{PodName: pod.Name, PodIp: pod.Status.PodIP, WorkerNode: pod.Spec.NodeName}
			klog.Info("PodData :", *podData, " PODIP:", pod.Status)
			populateWorkerPodList(podData)
		}
	}

}

func (npm *networkPerfMetricsMeasurement) gather(config *measurement.Config) (measurement.Summary, error) {
	npm.GetMetricsForDisp()
	content, err := util.PrettyPrintJSON(&measurementutil.PerfData{
		Version: "v1",
		// DataItems: []measurementutil.DataItem{latency.ToPerfData(p.String())}
		DataItems: networkPerfRespForDisp.DataItems,
	})
	if err != nil {
		klog.Info("Pretty Print to Json Err:", err)
	}
	return measurement.CreateSummary(npm.String()+networkPerfRespForDisp.Client_Server_Ratio+networkPerfRespForDisp.Protocol+networkPerfRespForDisp.Service, "json", content), nil
}

func (npm *networkPerfMetricsMeasurement) GetMetricsForDisp() {
	getMetricsForDisplay(npm.podRatio, npm.protocol)
}

func (npm *networkPerfMetricsMeasurement) validate(config *measurement.Config) error {
	var ratio, protocol string
	var duration int
	var err error

	if duration, err = util.GetInt(config.Params, "duration"); err != nil {
		return err
	}

	if ratio, err = util.GetString(config.Params, "ratio"); err != nil {
		return err
	}
	if protocol, err = util.GetString(config.Params, "protocol"); err != nil {
		return err
	}

	if protocol != Protocol_TCP && protocol != Protocol_UDP && protocol != Protocol_HTTP {
		return err
	}

	npm.testDuration = duration
	npm.podRatio = ratio
	npm.protocol = protocol
	return nil
}

// Dispose cleans up after the measurement.
func (npm *networkPerfMetricsMeasurement) Dispose() {
	if npm.framework == nil {
		klog.V(1).Infof("Network measurement %s wasn't started, skipping the Dispose() step", npm)
		return
	}
	klog.Info("Stopping %s network measurement...", npm)
	k8sClient := npm.framework.GetClientSets().GetClient()
	if err := client.DeleteNamespace(k8sClient, netperfNamespace); err != nil {
		klog.Errorf("error while deleting %s namespace: %v", netperfNamespace, err)
	}
	if err := client.WaitForDeleteNamespace(k8sClient, netperfNamespace); err != nil {
		klog.Errorf("error while waiting for %s namespace to be deleted: %v", netperfNamespace, err)
	}
}
