package common

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	networkPerfMetricsName = "NetworkPerformanceMetrics"
	netperfNamespace       = "netperf-1"
)

type NetworkPerfResp struct {
	Client_Server_Ratio string
	Protocol            string
	Service             string
	DataItems           []measurementutil.DataItem
}

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
		summary, err := m.gather(config)
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

func (m *networkPerfMetricsMeasurement) Dispose() {

}

func (m *networkPerfMetricsMeasurement) start(config *measurement.Config) {
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()
	if err := client.CreateNamespace(k8sClient, netperfNamespace); err != nil {
		klog.Info("Error starting measurement:", err)
	}
}

func (m *networkPerfMetricsMeasurement) gather(config *measurement.Config) (measurement.Summary, error) {
	body, queryErr := config.ClusterFramework.GetClientSets().GetClient().CoreV1().
		Services("netperf-1").
		ProxyGet("http", "controller-service-0", "5010", "/metrics", nil).
		DoRaw(context.TODO())
	if queryErr != nil {
		klog.Info("Error:", queryErr)
	}
	klog.Info("GOT RESPONSE:")
	klog.Info(string(body))
	////TODO to be removed/////////////
	// body = []byte(`{"Client_Server_Ratio":"1:1","Protocol":"TCP","Service":"P2P","dataItems":[{"data":{"value":1935.318591},"unit":"kbytes/sec","labels":{"Metric": "Throughput"}}] }`)
	///////////////////////////////////
	var dat NetworkPerfResp
	if err := json.Unmarshal(body, &dat); err != nil {
		panic(err)
	}
	fmt.Println(dat)

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

func (*networkPerfMetricsMeasurement) String() string {
	return networkPerfMetricsName
}
