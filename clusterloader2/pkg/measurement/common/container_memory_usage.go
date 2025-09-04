/*
Copyright 2025 The Kubernetes Authors.

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

package common

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	containerMemoryUsageName = "ContainerMemoryUsage"
)

func init() {
	if err := measurement.Register(containerMemoryUsageName, createContainerMemoryUsageMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", containerMemoryUsageName, err)
	}
	klog.Infof("[ContainerMemoryUsage] Starting measurement with label selector")
}

func createContainerMemoryUsageMeasurement() measurement.Measurement {
	return &containerMemoryUsageMeasurement{}
}

type containerMemoryUsageMeasurement struct {
	isRunning     bool
	summaries     []measurement.Summary
	clientset     *kubernetes.Clientset
	pollFrequency time.Duration
	threshold     float64

	stopCh  chan struct{}
	mu      sync.Mutex
	samples []float64
	lock    sync.Mutex
	wg      sync.WaitGroup
}

func (c *containerMemoryUsageMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	switch action {
	case "start":
		return nil, c.start(config)
	case "gather":
		return c.gather()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}

}

// Configure parses configuration and initializes clientset
func (c *containerMemoryUsageMeasurement) initFields(config *measurement.Config) error {
	c.isRunning = true
	c.stopCh = make(chan struct{})

	frequency, err := util.GetDuration(config.Params, "pollFrequency")
	if err != nil {
		return err
	}
	c.pollFrequency = frequency

	threshold, err := util.GetFloat64OrDefault(config.Params, "threshold", 0)
	if err != nil {
		return err
	}
	c.threshold = threshold

	return nil
}

func (c *containerMemoryUsageMeasurement) start(config *measurement.Config) error {
	klog.V(2).Infof("%v: measurement already running", c)
	if c.isRunning {
		klog.V(2).Infof("%v: measurement already running", c)
		return nil
	}
	if err := c.initFields(config); err != nil {
		return err
	}
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		for {
			select {
			case <-c.stopCh:
				return
			case <-time.After(c.pollFrequency):
				memoryUsage, err := c.getAPIServerMemoryUsage(k8sClient)
				if err != nil {
					klog.Warningf("ContainerMemoryUsage: read error: %v", err)
					continue
				}

				// convert Memory bytes to GB
				memoryInGB := memoryUsage / (1024 * 1024 * 1024)

				c.samples = append(c.samples, memoryInGB)
			}
		}
	}()
	return nil
}

func (c *containerMemoryUsageMeasurement) gather() ([]measurement.Summary, error) {
	klog.V(2).Infof("%v: measurement already running", c)
	if !c.isRunning {
		return nil, nil
	}

	// Close this channel to stop the execution
	close(c.stopCh)
	// Wait for execution goroutine to finish
	c.wg.Wait()
	c.isRunning = false

	klog.V(2).Infof("%s: gathering summaries", containerMemoryUsageName)
	maxMemoryUsage := slices.Max(c.samples)

	content := fmt.Sprintf(`{"maxMemoryUsage": %d, "threshold": %d}`, maxMemoryUsage, c.threshold)

	summary := measurement.CreateSummary(containerMemoryUsageName, "json", content)
	klog.V(2).Infof("%d: maxMemoryUsage", maxMemoryUsage)
	klog.V(2).Infof("%d: threshold", c.threshold)
	if maxMemoryUsage > c.threshold {
		err := errors.NewMetricViolationError("ContainerMemoryUsage", fmt.Sprintf("SLO not fulfilled (expected >= %f, got: %f)", c.threshold, maxMemoryUsage))
		return nil, err
	}

	return []measurement.Summary{summary}, nil

}

func (c *containerMemoryUsageMeasurement) getAPIServerMemoryUsage(cl clientset.Interface) (float64, error) {

	result, err := cl.CoreV1().RESTClient().
		Get().
		AbsPath("apis/metrics.k8s.io/v1beta1/namespaces/kube-system/pods").
		Param("labelSelector", "component=kube-apiserver").
		DoRaw(context.Background())

	scheme := runtime.NewScheme()
	_ = metricsapi.AddToScheme(scheme) // register metrics API types into scheme

	codef := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	if err != nil {
		return 0, err
	}

	podMetricsList := &metricsapi.PodMetricsList{}
	_, _, err = codef.Decode(result, nil, podMetricsList)
	if err != nil {
		return 0, fmt.Errorf("failed to decode metrics: %v", err)
	}

	var memoryUsage float64

	for _, podMetric := range podMetricsList.Items {
		for _, container := range podMetric.Containers {
			if container.Name == "kube-apiserver" {
				memoryUsage = container.Usage.Memory().AsApproximateFloat64()
				break
			}
		}
	}
	return memoryUsage, nil
}

func (c *containerMemoryUsageMeasurement) Dispose() {}

func (c *containerMemoryUsageMeasurement) String() string {
	return containerMemoryUsageName
}
