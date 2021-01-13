package common

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	measurementName = "PeriodicPodList"
	interval        = 2 * time.Minute
)

func init() {
	if err := measurement.Register(measurementName, createMeasurement); err != nil {
		klog.Fatalf("failed to register %q: %v", measurementName, err)
	}
}

type podLister struct {
	stopCh chan struct{}
}

func createMeasurement() measurement.Measurement {
	return &podLister{stopCh: make(chan struct{})}
}

func (s *podLister) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		return nil, s.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		return s.stop()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
	return nil, nil
}

func (s *podLister) start(c clientset.Interface) error {
	go wait.Until(func() {
		if err := s.list(c); err != nil {
			klog.Warningf("failed to list pods: %v", err)
		}
	}, interval, s.stopCh)
	return nil
}

func (s *podLister) list(c clientset.Interface) error {
	opts := metav1.ListOptions{
		ResourceVersion: "0",
	}
	start := time.Now()
	_, err := c.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), opts)
	klog.Infof("Listing pods took %v", time.Since(start))
	return err
}

func (s *podLister) stop() ([]measurement.Summary, error) {
	close(s.stopCh)
	return nil, nil
}

// Dispose cleans up after the measurement.
func (s *podLister) Dispose() {
	s.stop()
}

func (s *podLister) String() string {
	return measurementName
}
