package common

import (
	"context"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	networkPerfMetricsName = "NetworkPerformanceMetrics"
	netperfNamespace       = "netperf-1"
)

func init() {
	klog.Info("Registering Netowrk Measurement")
	if err := measurement.Register(networkPerfMetricsName, createNetworkPerfMetricsMeasurement); err != nil {
		klog.Fatal("Cannot register %s: %v", networkPerfMetricsName, err)
	}
}

func createNetworkPerfMetricsMeasurement() measurement.Measurement {
	return &networkPerfMetricsMeasurement{}
}

type networkPerfMetricsMeasurement struct {
}

type networkPerfMetrics struct {
	Name    string `json:"name"`
	Metrics []float64
}

func (m *networkPerfMetricsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {

	klog.Info("In network execute")
	action, err := util.GetString(config.Params, "action")
	klog.Info("In network execute:action:", action)
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		m.start(config)
	case "gather":
		m.gather(config)
	default:
	}

	return nil, nil
}

func (m *networkPerfMetricsMeasurement) Dispose() {

}

func (m *networkPerfMetricsMeasurement) start(config *measurement.Config) {
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()
	if err := client.CreateNamespace(k8sClient, netperfNamespace); err != nil {
		klog.Info("Error starting measurement:", err)
	}
}

func (m *networkPerfMetricsMeasurement) gather(config *measurement.Config) {
	body, queryErr := config.ClusterFramework.GetClientSets().GetClient().CoreV1().
		Services("netperf-1").
		ProxyGet("http", "controller-service-0", "5010", "/metrics", nil).
		DoRaw(context.TODO())
	if queryErr != nil {
		klog.Info("Error:", queryErr)
	}
	klog.Info("GOT RESPONSE:")
	klog.Info(string(body))
}

func (*networkPerfMetricsMeasurement) String() string {
	return networkPerfMetricsName
}
