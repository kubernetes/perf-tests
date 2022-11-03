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

package metrics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/cluster/ports"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	"k8s.io/klog/v2"
)

const (
	// kubeSchedulerPort is the default port for the scheduler status server.
	kubeSchedulerPort = 10259
)

// Collection is metrics collection of components
type Collection struct {
	APIServerMetrics         APIServerMetrics
	ControllerManagerMetrics ControllerManagerMetrics
	KubeletMetrics           map[string]KubeletMetrics
	SchedulerMetrics         SchedulerMetrics
	ClusterAutoscalerMetrics ClusterAutoscalerMetrics
}

// Grabber provides functions which grab metrics from components
type Grabber struct {
	client                            clientset.Interface
	externalClient                    clientset.Interface
	grabFromAPIServer                 bool
	grabFromControllerManager         bool
	grabFromKubelets                  bool
	grabFromScheduler                 bool
	grabFromClusterAutoscaler         bool
	masterName                        string
	registeredMaster                  bool
	waitForControllerManagerReadyOnce sync.Once
}

// deprecatedMightBeMasterNode returns true if given node is a registered master.
// This code must not be updated to use node role labels, since node role labels
// may not change behavior of the system.
// It has been copied from https://github.com/kubernetes/kubernetes/blob/9e991415386e4cf155a24b1da15becaa390438d8/test/e2e/system/system_utils.go#L27
// as it has been used in future k8s versions.
// TODO(mborsz): Remove dependency on this function.
func deprecatedMightBeMasterNode(nodeName string) bool {
	// We are trying to capture "master(-...)?$" regexp.
	// However, using regexp.MatchString() results even in more than 35%
	// of all space allocations in ControllerManager spent in this function.
	// That's why we are trying to be a bit smarter.
	if strings.HasSuffix(nodeName, "master") {
		return true
	}
	if len(nodeName) >= 10 {
		return strings.HasSuffix(nodeName[:len(nodeName)-3], "master-")
	}
	return false
}

// NewMetricsGrabber returns new metrics which are initialized.
func NewMetricsGrabber(c clientset.Interface, ec clientset.Interface, kubelets bool, scheduler bool, controllers bool, apiServer bool, clusterAutoscaler bool) (*Grabber, error) {
	registeredMaster := false
	masterName := ""
	nodeList, err := c.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(nodeList.Items) < 1 {
		klog.Warning("Can't find any Nodes in the API server to grab metrics from")
	}
	for _, node := range nodeList.Items {
		if deprecatedMightBeMasterNode(node.Name) {
			registeredMaster = true
			masterName = node.Name
			break
		}
	}
	if !registeredMaster {
		scheduler = false
		controllers = false
		clusterAutoscaler = ec != nil
		if clusterAutoscaler {
			klog.Warningf("Master node is not registered. Grabbing metrics from Scheduler, ControllerManager is disabled.")
		} else {
			klog.Warningf("Master node is not registered. Grabbing metrics from Scheduler, ControllerManager and ClusterAutoscaler is disabled.")
		}
	}

	return &Grabber{
		client:                    c,
		externalClient:            ec,
		grabFromAPIServer:         apiServer,
		grabFromControllerManager: controllers,
		grabFromKubelets:          kubelets,
		grabFromScheduler:         scheduler,
		grabFromClusterAutoscaler: clusterAutoscaler,
		masterName:                masterName,
		registeredMaster:          registeredMaster,
	}, nil
}

// HasRegisteredMaster returns if metrics grabber was able to find a master node
func (g *Grabber) HasRegisteredMaster() bool {
	return g.registeredMaster
}

// GrabFromKubelet returns metrics from kubelet
func (g *Grabber) GrabFromKubelet(nodeName string) (KubeletMetrics, error) {
	nodes, err := g.client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{FieldSelector: fields.Set{"metadata.name": nodeName}.AsSelector().String()})
	if err != nil {
		return KubeletMetrics{}, err
	}
	if len(nodes.Items) != 1 {
		return KubeletMetrics{}, fmt.Errorf("error listing nodes with name %v, got %v", nodeName, nodes.Items)
	}
	kubeletPort := nodes.Items[0].Status.DaemonEndpoints.KubeletEndpoint.Port
	return g.grabFromKubeletInternal(nodeName, int(kubeletPort))
}

func (g *Grabber) grabFromKubeletInternal(nodeName string, kubeletPort int) (KubeletMetrics, error) {
	if kubeletPort <= 0 || kubeletPort > 65535 {
		return KubeletMetrics{}, fmt.Errorf("invalid Kubelet port %v. Skipping Kubelet's metrics gathering", kubeletPort)
	}
	output, err := g.getMetricsFromNode(nodeName, int(kubeletPort))
	if err != nil {
		return KubeletMetrics{}, err
	}
	return parseKubeletMetrics(output)
}

// GrabFromScheduler returns metrics from scheduler
func (g *Grabber) GrabFromScheduler() (SchedulerMetrics, error) {
	if !g.registeredMaster {
		return SchedulerMetrics{}, fmt.Errorf("master's Kubelet is not registered. Skipping Scheduler's metrics gathering")
	}
	output, err := g.getMetricsFromPod(g.client, fmt.Sprintf("%v-%v", "kube-scheduler", g.masterName), metav1.NamespaceSystem, kubeSchedulerPort, true)
	if err != nil {
		return SchedulerMetrics{}, err
	}
	return parseSchedulerMetrics(output)
}

// GrabFromClusterAutoscaler returns metrics from cluster autoscaler
func (g *Grabber) GrabFromClusterAutoscaler() (ClusterAutoscalerMetrics, error) {
	if !g.registeredMaster && g.externalClient == nil {
		return ClusterAutoscalerMetrics{}, fmt.Errorf("master's Kubelet is not registered. Skipping ClusterAutoscaler's metrics gathering")
	}
	var client clientset.Interface
	var namespace string
	if g.externalClient != nil {
		client = g.externalClient
		namespace = "kubemark"
	} else {
		client = g.client
		namespace = metav1.NamespaceSystem
	}
	output, err := g.getMetricsFromPod(client, "cluster-autoscaler", namespace, 8085, false)
	if err != nil {
		return ClusterAutoscalerMetrics{}, err
	}
	return parseClusterAutoscalerMetrics(output)
}

// GrabFromControllerManager returns metrics from controller manager
func (g *Grabber) GrabFromControllerManager() (ControllerManagerMetrics, error) {
	if !g.registeredMaster {
		return ControllerManagerMetrics{}, fmt.Errorf("master's Kubelet is not registered. Skipping ControllerManager's metrics gathering")
	}

	var err error
	podName := fmt.Sprintf("%v-%v", "kube-controller-manager", g.masterName)
	g.waitForControllerManagerReadyOnce.Do(func() {
		if readyErr := e2epod.WaitTimeoutForPodReadyInNamespace(g.client, podName, metav1.NamespaceSystem, framework.PodStartTimeout); readyErr != nil {
			err = fmt.Errorf("error waiting for controller manager pod to be ready: %w", readyErr)
			return
		}

		var lastMetricsFetchErr error
		if metricsWaitErr := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
			_, lastMetricsFetchErr = g.getMetricsFromPod(g.client, podName, metav1.NamespaceSystem, ports.KubeControllerManagerPort, true)
			return lastMetricsFetchErr == nil, nil
		}); metricsWaitErr != nil {
			err = fmt.Errorf("error waiting for controller manager pod to expose metrics: %v; %v", metricsWaitErr, lastMetricsFetchErr)
			return
		}
	})
	if err != nil {
		return ControllerManagerMetrics{}, err
	}

	output, err := g.getMetricsFromPod(g.client, podName, metav1.NamespaceSystem, ports.KubeControllerManagerPort, true)
	if err != nil {
		return ControllerManagerMetrics{}, err
	}
	return parseControllerManagerMetrics(output)
}

// GrabFromAPIServer returns metrics from API server
func (g *Grabber) GrabFromAPIServer() (APIServerMetrics, error) {
	output, err := g.getMetricsFromAPIServer()
	if err != nil {
		return APIServerMetrics{}, nil
	}
	return parseAPIServerMetrics(output)
}

// Grab returns metrics from corresponding component
func (g *Grabber) Grab() (Collection, error) {
	result := Collection{}
	var errs []error
	if g.grabFromAPIServer {
		metrics, err := g.GrabFromAPIServer()
		if err != nil {
			errs = append(errs, err)
		} else {
			result.APIServerMetrics = metrics
		}
	}
	if g.grabFromScheduler {
		metrics, err := g.GrabFromScheduler()
		if err != nil {
			errs = append(errs, err)
		} else {
			result.SchedulerMetrics = metrics
		}
	}
	if g.grabFromControllerManager {
		metrics, err := g.GrabFromControllerManager()
		if err != nil {
			errs = append(errs, err)
		} else {
			result.ControllerManagerMetrics = metrics
		}
	}
	if g.grabFromClusterAutoscaler {
		metrics, err := g.GrabFromClusterAutoscaler()
		if err != nil {
			errs = append(errs, err)
		} else {
			result.ClusterAutoscalerMetrics = metrics
		}
	}
	if g.grabFromKubelets {
		result.KubeletMetrics = make(map[string]KubeletMetrics)
		nodes, err := g.client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			errs = append(errs, err)
		} else {
			for _, node := range nodes.Items {
				kubeletPort := node.Status.DaemonEndpoints.KubeletEndpoint.Port
				metrics, err := g.grabFromKubeletInternal(node.Name, int(kubeletPort))
				if err != nil {
					errs = append(errs, err)
				}
				result.KubeletMetrics[node.Name] = metrics
			}
		}
	}
	if len(errs) > 0 {
		return result, fmt.Errorf("errors while grabbing metrics: %v", errs)
	}
	return result, nil
}

func (g *Grabber) getMetricsFromPod(client clientset.Interface, podName string, namespace string, port int, enableHTTPS bool) (string, error) {
	var name string
	if enableHTTPS {
		name = fmt.Sprintf("https:%s:%d", podName, port)
	} else {
		name = fmt.Sprintf("%s:%d", podName, port)
	}
	rawOutput, err := client.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(name).
		Suffix("metrics").
		Do(context.TODO()).Raw()
	if err != nil {
		return "", err
	}
	return string(rawOutput), nil
}
