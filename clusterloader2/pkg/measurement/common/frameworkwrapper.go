package common

import (
	"fmt"

	"github.com/golang/glog"
	framework "k8s.io/kubernetes/test/e2e/framework"
	_ "k8s.io/kubernetes/test/utils"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func init() {
	measurement.Register("SchedulerLatency", createSchedulerMetricsMeasurement)
}

func createSchedulerMetricsMeasurement() measurement.Measurement {
	return &schedulerMetricsMeasurement{}
}

type schedulerMetricsMeasurement struct{}

func (*schedulerMetricsMeasurement) Execute(config *measurement.MeasurementConfig) error {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return err
	}

	switch action {
	case "reset":
		return framework.ResetSchedulerMetrics(config.ClientSet)
	case "gather":
		latency, err := framework.VerifySchedulerLatency(config.ClientSet)
		if err != nil {
			return err
		}
		glog.Infof("SchedulerLatency: %v", latency.PrintHumanReadable())
	default:
		return fmt.Errorf("SchedulerLatency: unknown action %v", action)
	}
	return nil
}
