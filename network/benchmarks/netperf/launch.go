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
	"os"
	"strings"
	"time"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	csvDataMarker    = "GENERATING CSV OUTPUT"
	csvEndDataMarker = "END CSV DATA"
	runUUID          = "latest"
	orchestratorPort = 5202
	iperf3Port       = 5201
	qperf19766       = 19766
	qperf19765       = 19765
	netperfPort      = 12865
)

var (
	iterations     int
	hostnetworking bool
	tag            string
	kubeConfig     string
	testNamespace  string
	netperfImage   string
	cleanupOnly    bool

	everythingSelector metav1.ListOptions = metav1.ListOptions{}

	primaryNode   api.Node
	secondaryNode api.Node

	testFrom, testTo int
)

func init() {
	flag.BoolVar(&hostnetworking, "hostnetworking", false,
		"(boolean) Enable Host Networking Mode for PODs")
	flag.IntVar(&iterations, "iterations", 1,
		"Number of iterations to run")
	flag.StringVar(&tag, "tag", runUUID, "CSV file suffix")
	flag.StringVar(&netperfImage, "image", "sirot/netperf-latest", "Docker image used to run the network tests")
	flag.StringVar(&testNamespace, "namespace", "netperf", "Test namespace to run netperf pods")
	defaultKubeConfig := fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	flag.StringVar(&kubeConfig, "kubeConfig", defaultKubeConfig,
		"Location of the kube configuration file ($HOME/.kube/config")
	flag.BoolVar(&cleanupOnly, "cleanup", false,
		"(boolean) Run the cleanup resources phase only (use this flag to clean up orphaned resources from a test run)")
	flag.IntVar(&testFrom, "testFrom", 0, "start from test number testFrom")
	flag.IntVar(&testTo, "testTo", 5, "end at test number testTo")
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
	nodes, err := c.CoreV1().Nodes().List(
		metav1.ListOptions{
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
	rcs, err := c.CoreV1().ReplicationControllers(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get replication controllers", err)
		return
	}
	for _, rc := range rcs.Items {
		fmt.Println("Deleting rc", rc.GetName())
		if err := c.CoreV1().ReplicationControllers(testNamespace).Delete(
			rc.GetName(), &metav1.DeleteOptions{}); err != nil {
			fmt.Println("Failed to delete rc", rc.GetName(), err)
		}
	}
	pods, err := c.CoreV1().Pods(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get pods", err)
		return
	}
	for _, pod := range pods.Items {
		fmt.Println("Deleting pod", pod.GetName())
		if err := c.CoreV1().Pods(testNamespace).Delete(pod.GetName(), &metav1.DeleteOptions{GracePeriodSeconds: new(int64)}); err != nil {
			fmt.Println("Failed to delete pod", pod.GetName(), err)
		}
	}
	svcs, err := c.CoreV1().Services(testNamespace).List(everythingSelector)
	if err != nil {
		fmt.Println("Failed to get services", err)
		return
	}
	for _, svc := range svcs.Items {
		fmt.Println("Deleting svc", svc.GetName())
		err := c.CoreV1().Services(testNamespace).Delete(
			svc.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			fmt.Println("Failed to get service", err)
		}
	}
}

// createServices: Long-winded function to programmatically create our two services
func createServices(c *kubernetes.Clientset) bool {
	// Create our namespace if not present
	if _, err := c.CoreV1().Namespaces().Get(testNamespace, metav1.GetOptions{}); err != nil {
		_, err := c.CoreV1().Namespaces().Create(&api.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
		if err != nil {
			fmt.Println("Failed to create service", err)
		}
	}

	// Create the orchestrator service that points to the coordinator pod
	orchLabels := map[string]string{"app": "netperf-orch"}
	orchService := &api.Service{
		ObjectMeta: metav1.ObjectMeta{
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
	if _, err := c.CoreV1().Services(testNamespace).Create(orchService); err != nil {
		fmt.Println("Failed to create orchestrator service", err)
		return false
	}
	fmt.Println("Created orchestrator service")

	// Create the netperf-w2 service that points a clusterIP at the worker 2 pod
	netperfW2Labels := map[string]string{"app": "netperf-w2"}
	netperfW2Service := &api.Service{
		ObjectMeta: metav1.ObjectMeta{
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
					Name:       "netperf-w2-qperf19766",
					Protocol:   api.ProtocolTCP,
					Port:       qperf19766,
					TargetPort: intstr.FromInt(qperf19766),
				},
				{
					Name:       "netperf-w2-qperf19765",
					Protocol:   api.ProtocolTCP,
					Port:       qperf19765,
					TargetPort: intstr.FromInt(qperf19765),
				},
				{
					Name:       "netperf-w2-sctp",
					Protocol:   api.ProtocolSCTP,
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
	if _, err := c.CoreV1().Services(testNamespace).Create(netperfW2Service); err != nil {
		fmt.Println("Failed to create netperf-w2 service", err)
		return false
	}
	fmt.Println("Created netperf-w2 service")
	return true
}

// createRCs - Create replication controllers for all workers and the orchestrator
func createRCs(c *kubernetes.Clientset) bool {
	// Create the orchestrator RC
	name := "netperf-orch"
	fmt.Println("Creating replication controller", name)
	replicas := int32(1)

	_, err := c.CoreV1().ReplicationControllers(testNamespace).Create(&api.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: api.ReplicationControllerSpec{
			Replicas: &replicas,
			Selector: map[string]string{"app": name},
			Template: &api.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Name:  name,
							Image: netperfImage,
							Ports: []api.ContainerPort{{ContainerPort: orchestratorPort}},
							Args: []string{
								"--mode=orchestrator",
								fmt.Sprintf("--testFrom=%d", testFrom),
								fmt.Sprintf("--testTo=%d", testTo),
							},
							ImagePullPolicy: "Always",
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
			portSpec = append(portSpec, api.ContainerPort{ContainerPort: iperf3Port, Protocol: api.ProtocolSCTP})
		}

		workerEnv := []api.EnvVar{
			{Name: "worker", Value: name},
			{Name: "kubeNode", Value: kubeNode},
			{Name: "podname", Value: name},
		}

		replicas := int32(1)

		_, err := c.CoreV1().ReplicationControllers(testNamespace).Create(&api.ReplicationController{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: api.ReplicationControllerSpec{
				Replicas: &replicas,
				Selector: map[string]string{"app": name},
				Template: &api.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
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
	body, err := c.CoreV1().Pods(testNamespace).GetLogs(podName, &api.PodLogOptions{Timestamps: false}).DoRaw()
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
	t := time.Now().UTC()
	outputFileDirectory := fmt.Sprintf("results_%s-%s", testNamespace, tag)
	outputFilePrefix := fmt.Sprintf("%s-%s_%s.", testNamespace, tag, t.Format("20060102150405"))
	fmt.Printf("Test concluded - CSV raw data written to %s/%scsv\n", outputFileDirectory, outputFilePrefix)
	if _, err := os.Stat(outputFileDirectory); os.IsNotExist(err) {
		err := os.Mkdir(outputFileDirectory, 0766)
		if err != nil {
			fmt.Println("Error creating directory", err)
			return false
		}

	}
	fd, err := os.OpenFile(fmt.Sprintf("%s/%scsv", outputFileDirectory, outputFilePrefix), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println("ERROR writing output CSV datafile", err)
		return false
	}
	_, err = fd.WriteString(*csvData)
	if err != nil {
		fmt.Println("Error writing string", err)
		return false
	}
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
			if pods, err = c.CoreV1().Pods(testNamespace).List(everythingSelector); err != nil {
				fmt.Println("Failed to fetch pods - waiting for pod creation", err)
				continue
			}
			orchestratorPodName = getOrchestratorPodName(pods)
		}
		fmt.Println("Orchestrator Pod is", orchestratorPodName)

		// The pods orchestrate themselves, we just wait for the results file to show up in the orchestrator container
		for {
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
	fmt.Println("Test Namespace  : ", testNamespace)
	fmt.Println("Docker image    : ", netperfImage)
	fmt.Println("------------------------------------------------------------")

	var c *kubernetes.Clientset
	if c = setupClient(); c == nil {
		fmt.Println("Failed to setup REST client to Kubernetes cluster")
		return
	}
	if cleanupOnly {
		cleanup(c)
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
