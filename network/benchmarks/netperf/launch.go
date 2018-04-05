/*
Copyright 2016 The Kubernetes Authors.

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

/*
 launch.go

 Launch the netperf tests

 1. Launch the netperf-orch service
 2. Launch the worker pods
 3. Wait for the output csv data to show up in orchestrator pod logs
*/

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	metav1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/util/intstr"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	debugLog         = "output.txt"
	testNamespace    = "netperf"
	csvDataMarker    = "GENERATING CSV OUTPUT"
	csvEndDataMarker = "END CSV DATA"
	runUUID          = "latest"
	orchestratorPort = 5202
	iperf3Port       = 5201
	netperfPort      = 12865
)

var (
	iterations     int
	hostnetworking bool
	tag            string
	kubeConfig     string
	netperfImage   string

	everythingSelector api.ListOptions = api.ListOptions{}

	primaryNode   api.Node
	secondaryNode api.Node
)

func init() {
	flag.BoolVar(&hostnetworking, "hostnetworking", false,
		"(boolean) Enable Host Networking Mode for PODs")
	flag.IntVar(&iterations, "iterations", 1,
		"Number of iterations to run")
	flag.StringVar(&tag, "tag", runUUID, "CSV file suffix")
	flag.StringVar(&netperfImage, "image", "sirot/netperf-latest", "Docker image used to run the network tests")
	flag.StringVar(&kubeConfig, "kubeConfig", "",
		"Location of the kube configuration file ($HOME/.kube/config")
}

func setupClient() *kubernetes.Clientset {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		panic(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientset
}

// getMinions : Only return schedulable/worker nodes
func getMinionNodes(c *kubernetes.Clientset) *api.NodeList {
	nodes, err := c.Nodes().List(
		api.ListOptions{
			FieldSelector: "spec.unschedulable=false",
		})
	if err != nil {
		fmt.Println("Failed to fetch nodes", err)
		return nil
	}
	return nodes
}

func cleanup(c *kubernetes.Clientset) {
	// Cleanup existing rcs, pods and services in our namespace
	cfgm, err := c.ConfigMaps(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get config maps", err)
		return
	}
	for _, cm := range cfgm.Items {
		fmt.Println("Deleting configMap", cm.GetName())
		if err := c.ConfigMaps(testNamespace).Delete(
			cm.GetName(), &api.DeleteOptions{}); err != nil {
			fmt.Println("Failed to delete configmap", cm.GetName(), err)
		}
	}

	rcs, err := c.ReplicationControllers(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get replication controllers", err)
		return
	}
	for _, rc := range rcs.Items {
		fmt.Println("Deleting rc", rc.GetName())
		if err := c.ReplicationControllers(testNamespace).Delete(
			rc.GetName(), &api.DeleteOptions{}); err != nil {
			fmt.Println("Failed to delete rc", rc.GetName(), err)
		}
	}
	pods, err := c.Pods(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get pods", err)
		return
	}
	for _, pod := range pods.Items {
		fmt.Println("Deleting pod", pod.GetName())
		if err := c.Pods(testNamespace).Delete(pod.GetName(), &api.DeleteOptions{GracePeriodSeconds: new(int64)}); err != nil {
			fmt.Println("Failed to delete pod", pod.GetName(), err)
		}
	}
	svcs, err := c.Services(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get services", err)
		return
	}
	for _, svc := range svcs.Items {
		fmt.Println("Deleting svc", svc.GetName())
		c.Services(testNamespace).Delete(
			svc.GetName(), &api.DeleteOptions{})
	}
}

// createServices: Long-winded function to programmatically create our two services
func createServices(c *kubernetes.Clientset) bool {
	// Create our namespace if not present
	if _, err := c.Namespaces().Get(testNamespace, metav1.GetOptions{}); err != nil {
		c.Namespaces().Create(&api.Namespace{ObjectMeta: api.ObjectMeta{Name: testNamespace}})
	}

	// Create the orchestrator service that points to the coordinator pod
	orchLabels := map[string]string{"app": "netperf-orch"}
	orchService := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name: "netperf-orch",
		},
		Spec: api.ServiceSpec{
			Selector: orchLabels,
			Ports: []api.ServicePort{{
				Name:       "netperf-orch",
				Protocol:   api.ProtocolTCP,
				Port:       orchestratorPort,
				TargetPort: intstr.FromInt(orchestratorPort),
			}},
			Type: api.ServiceTypeClusterIP,
		},
	}
	if _, err := c.Services(testNamespace).Create(orchService); err != nil {
		fmt.Println("Failed to create orchestrator service", err)
		return false
	}
	fmt.Println("Created orchestrator service")

	// Create the netperf-w2 service that points a clusterIP at the worker 2 pod
	netperfW2Labels := map[string]string{"app": "netperf-w2"}
	netperfW2Service := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name: "netperf-w2",
		},
		Spec: api.ServiceSpec{
			Selector: netperfW2Labels,
			Ports: []api.ServicePort{
				{
					Name:       "netperf-w2",
					Protocol:   api.ProtocolTCP,
					Port:       iperf3Port,
					TargetPort: intstr.FromInt(iperf3Port),
				},
				{
					Name:       "netperf-w2-udp",
					Protocol:   api.ProtocolUDP,
					Port:       iperf3Port,
					TargetPort: intstr.FromInt(iperf3Port),
				},
				{
					Name:       "netperf-w2-netperf",
					Protocol:   api.ProtocolTCP,
					Port:       netperfPort,
					TargetPort: intstr.FromInt(netperfPort),
				},
			},
			Type: api.ServiceTypeClusterIP,
		},
	}
	if _, err := c.Services(testNamespace).Create(netperfW2Service); err != nil {
		fmt.Println("Failed to create netperf-w2 service", err)
		return false
	}
	fmt.Println("Created netperf-w2 service")
	return true
}

func createConfigMap(c *kubernetes.Clientset) bool {
	fmt.Println("Creating configMap for scenario tests")
	scenarioConfig := "configmap/scenarios.json"
	data, err := ioutil.ReadFile(scenarioConfig)
	if err != nil {
		fmt.Println("Error opening config file", err)
		return false
	}

	_, err = c.ConfigMaps(testNamespace).Create(&api.ConfigMap{
		ObjectMeta: api.ObjectMeta{Name: "scenario-config"},
		Data:       map[string]string{"scenarios.json": string(data)},
	})
	if err != nil {
		fmt.Println("Error creating configMap", err)
		return false
	}
	return true
}

// createRCs - Create replication controllers for all workers and the orchestrator
func createRCs(c *kubernetes.Clientset) bool {
	// Create the orchestrator RC
	name := "netperf-orch"
	fmt.Println("Creating replication controller", name)
	replicas := int32(1)

	_, err := c.ReplicationControllers(testNamespace).Create(&api.ReplicationController{
		ObjectMeta: api.ObjectMeta{Name: name},
		Spec: api.ReplicationControllerSpec{
			Replicas: &replicas,
			Selector: map[string]string{"app": name},
			Template: &api.PodTemplateSpec{
				ObjectMeta: api.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Name:            name,
							Image:           netperfImage,
							Ports:           []api.ContainerPort{{ContainerPort: orchestratorPort}},
							Args:            []string{"--mode=orchestrator"},
							ImagePullPolicy: "Always",
							VolumeMounts: []api.VolumeMount{
								{
									Name:      "config",
									ReadOnly:  true,
									MountPath: "/config",
								},
							},
						},
					},
					Volumes: []api.Volume{
						{
							Name: "config",
							VolumeSource: api.VolumeSource{
								ConfigMap: &api.ConfigMapVolumeSource{LocalObjectReference: api.LocalObjectReference{Name: "scenario-config"},
									Items: []api.KeyToPath{{Key: "scenarios.json", Path: "scenarios.json"}},
								},
							},
						},
					},
					TerminationGracePeriodSeconds: new(int64),
				},
			},
		},
	})
	if err != nil {
		fmt.Println("Error creating orchestrator replication controller", err)
		return false
	}
	fmt.Println("Created orchestrator replication controller")
	for i := 1; i <= 3; i++ {
		// Bring up pods slowly
		time.Sleep(3 * time.Second)
		kubeNode := primaryNode.GetName()
		if i == 3 {
			kubeNode = secondaryNode.GetName()
		}
		name = fmt.Sprintf("netperf-w%d", i)
		fmt.Println("Creating replication controller", name)
		portSpec := []api.ContainerPort{}
		if i > 1 {
			// Worker W1 is a client-only pod - no ports are exposed
			portSpec = append(portSpec, api.ContainerPort{ContainerPort: iperf3Port, Protocol: api.ProtocolTCP})
		}

		workerEnv := []api.EnvVar{
			{Name: "worker", Value: name},
			{Name: "kubeNode", Value: kubeNode},
			{Name: "podname", Value: name},
		}

		replicas := int32(1)

		_, err := c.ReplicationControllers(testNamespace).Create(&api.ReplicationController{
			ObjectMeta: api.ObjectMeta{Name: name},
			Spec: api.ReplicationControllerSpec{
				Replicas: &replicas,
				Selector: map[string]string{"app": name},
				Template: &api.PodTemplateSpec{
					ObjectMeta: api.ObjectMeta{
						Labels: map[string]string{"app": name},
					},
					Spec: api.PodSpec{
						NodeName: kubeNode,
						Containers: []api.Container{
							{
								Name:            name,
								Image:           netperfImage,
								Ports:           portSpec,
								Args:            []string{"--mode=worker"},
								Env:             workerEnv,
								ImagePullPolicy: "Always",
							},
						},
						TerminationGracePeriodSeconds: new(int64),
					},
				},
			},
		})
		if err != nil {
			fmt.Println("Error creating orchestrator replication controller", name, ":", err)
			return false
		}
	}

	return true
}

func getOrchestratorPodName(pods *api.PodList) string {
	for _, pod := range pods.Items {
		if strings.Contains(pod.GetName(), "netperf-orch-") {
			return pod.GetName()
		}
	}
	return ""
}

// Retrieve the logs for the pod/container and check if csv data has been generated
func getCsvResultsFromPod(c *kubernetes.Clientset, podName string) *string {
	body, err := c.Pods(testNamespace).GetLogs(podName, &api.PodLogOptions{Timestamps: false}).DoRaw()
	if err != nil {
		fmt.Printf("Error (%s) reading logs from pod %s", err, podName)
		return nil
	}
	logData := string(body)
	index := strings.Index(logData, csvDataMarker)
	endIndex := strings.Index(logData, csvEndDataMarker)
	if index == -1 || endIndex == -1 {
		return nil
	}
	csvData := string(body[index+len(csvDataMarker)+1 : endIndex])
	return &csvData
}

// processCsvData : Process the CSV datafile and generate line and bar graphs
func processCsvData(csvData *string) bool {
	outputFilePrefix := fmt.Sprintf("%s-%s.", testNamespace, tag)
	fmt.Printf("Test concluded - CSV raw data written to %s.csv\n", outputFilePrefix)
	fd, err := os.OpenFile(fmt.Sprintf("%scsv", outputFilePrefix), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println("ERROR writing output CSV datafile", err)
		return false
	}
	fd.WriteString(*csvData)
	fd.Close()
	return true
}

func executeTests(c *kubernetes.Clientset) bool {
	for i := 0; i < iterations; i++ {
		cleanup(c)
		if !createServices(c) {
			fmt.Println("Failed to create services - aborting test")
			return false
		}
		time.Sleep(3 * time.Second)
		if !createConfigMap(c) {
			fmt.Println("Failed to create configMap for orchestrator")
			return false
		}
		if !createRCs(c) {
			fmt.Println("Failed to create replication controllers - aborting test")
			return false
		}
		fmt.Println("Waiting for netperf pods to start up")

		var orchestratorPodName string
		for len(orchestratorPodName) == 0 {
			fmt.Println("Waiting for orchestrator pod creation")
			time.Sleep(60 * time.Second)
			var pods *api.PodList
			var err error
			if pods, err = c.Pods(testNamespace).List(everythingSelector); err != nil {
				fmt.Println("Failed to fetch pods - waiting for pod creation", err)
				continue
			}
			orchestratorPodName = getOrchestratorPodName(pods)
		}
		fmt.Println("Orchestrator Pod is", orchestratorPodName)

		// The pods orchestrate themselves, we just wait for the results file to show up in the orchestrator container
		for true {
			// Monitor the orchestrator pod for the CSV results file
			csvdata := getCsvResultsFromPod(c, orchestratorPodName)
			if csvdata == nil {
				fmt.Println("Scanned orchestrator pod filesystem - no results file found yet...waiting for orchestrator to write CSV file...")
				time.Sleep(60 * time.Second)
				continue
			}
			if processCsvData(csvdata) {
				break
			}
		}
		fmt.Printf("TEST RUN (Iteration %d) FINISHED - cleaning up services and pods\n", i)
	}
	return false
}

func main() {
	flag.Parse()
	fmt.Println("Network Performance Test")
	fmt.Println("Parameters :")
	fmt.Println("Iterations      : ", iterations)
	fmt.Println("Host Networking : ", hostnetworking)
	fmt.Println("Docker image    : ", netperfImage)
	fmt.Println("------------------------------------------------------------")

	var c *kubernetes.Clientset
	if c = setupClient(); c == nil {
		fmt.Println("Failed to setup REST client to Kubernetes cluster")
		return
	}
	nodes := getMinionNodes(c)
	if nodes == nil {
		return
	}
	if len(nodes.Items) < 2 {
		fmt.Println("Insufficient number of nodes for test (need minimum 2 nodes)")
		return
	}
	primaryNode = nodes.Items[0]
	secondaryNode = nodes.Items[1]
	fmt.Printf("Selected primary,secondary nodes = (%s, %s)\n", primaryNode.GetName(), secondaryNode.GetName())
	executeTests(c)
	cleanup(c)
}
