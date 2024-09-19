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
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ritwikranjan/perf-tests/network/benchmarks/netperf/experiment"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	csvDataMarker     = "GENERATING CSV OUTPUT"
	csvEndDataMarker  = "END CSV DATA"
	jsonDataMarker    = "GENRATING JSON OUTPUT"
	jsonEndDataMarker = "END JSON OUTPUT"
	runUUID           = "latest"
	orchestratorPort  = 5202
	iperf3Port        = 5201
	qperf19766        = 19766
	qperf19765        = 19765
	netperfPort       = 12865
)

var (
	iterations    int
	tag           string
	kubeConfig    string
	testNamespace string
	netperfImage  string
	cleanupOnly   bool

	everythingSelector metav1.ListOptions = metav1.ListOptions{}

	primaryNode   api.Node
	secondaryNode api.Node

	testFrom, testTo int

	jsonOutput bool
)

func init() {
	flag.IntVar(&iterations, "iterations", 1,
		"Number of iterations to run")
	flag.StringVar(&tag, "tag", runUUID, "Result file suffix")
	flag.StringVar(&netperfImage, "image", "sirot/netperf-latest", "Docker image used to run the network tests")
	flag.StringVar(&testNamespace, "namespace", "netperf", "Test namespace to run netperf pods")
	defaultKubeConfig := fmt.Sprintf("%s/.kube/config", os.Getenv("HOME"))
	flag.StringVar(&kubeConfig, "kubeConfig", defaultKubeConfig,
		"Location of the kube configuration file ($HOME/.kube/config")
	flag.BoolVar(&cleanupOnly, "cleanup", false,
		"(boolean) Run the cleanup resources phase only (use this flag to clean up orphaned resources from a test run)")
	flag.IntVar(&testFrom, "testFrom", 0, "start from test number testFrom")
	flag.IntVar(&testTo, "testTo", 5, "end at test number testTo")
	flag.BoolVar(&jsonOutput, "json", false, "Output JSON data along with CSV data")
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

// getMinions : Only return schedulable/worker linux nodes
func getMinionNodes(c *kubernetes.Clientset) *api.NodeList {
	nodes, err := c.CoreV1().Nodes().List(
		context.Background(),
		metav1.ListOptions{
			FieldSelector: "spec.unschedulable=false",
			LabelSelector: "kubernetes.io/os=linux",
		})
	if err != nil {
		fmt.Println("Failed to fetch nodes", err)
		return nil
	}
	experiment.HelloWorld()
	return nodes
}

func cleanup(c *kubernetes.Clientset) {
	syncCtx := context.Background()
	// Cleanup existing rcs, pods and services in our namespace
	rcs, err := c.CoreV1().ReplicationControllers(testNamespace).List(syncCtx, everythingSelector)
	if err != nil {
		fmt.Println("Failed to get replication controllers", err)
		return
	}
	for _, rc := range rcs.Items {
		fmt.Println("Deleting rc", rc.GetName())
		if err := c.CoreV1().ReplicationControllers(testNamespace).Delete(
			context.Background(),
			rc.GetName(), metav1.DeleteOptions{}); err != nil {
			fmt.Println("Failed to delete rc", rc.GetName(), err)
		}
	}
	pods, err := c.CoreV1().Pods(testNamespace).List(syncCtx, everythingSelector)
	if err != nil {
		fmt.Println("Failed to get pods", err)
		return
	}
	for _, pod := range pods.Items {
		fmt.Println("Deleting pod", pod.GetName())
		if err := c.CoreV1().Pods(testNamespace).Delete(context.Background(), pod.GetName(), metav1.DeleteOptions{GracePeriodSeconds: new(int64)}); err != nil {
			fmt.Println("Failed to delete pod", pod.GetName(), err)
		}
	}
	svcs, err := c.CoreV1().Services(testNamespace).List(syncCtx, everythingSelector)
	if err != nil {
		fmt.Println("Failed to get services", err)
		return
	}
	for _, svc := range svcs.Items {
		fmt.Println("Deleting svc", svc.GetName())
		err := c.CoreV1().Services(testNamespace).Delete(
			context.Background(), svc.GetName(), metav1.DeleteOptions{})
		if err != nil {
			fmt.Println("Failed to get service", err)
		}
	}
}

// createServices: Long-winded function to programmatically create our two services
func createServices(c *kubernetes.Clientset) bool {
	// Create our namespace if not present
	if _, err := c.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{}); err != nil {
		_, err := c.CoreV1().Namespaces().Create(context.Background(), &api.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}, metav1.CreateOptions{})
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
	if _, err := c.CoreV1().Services(testNamespace).Create(context.Background(), orchService, metav1.CreateOptions{}); err != nil {
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
	if _, err := c.CoreV1().Services(testNamespace).Create(context.Background(), netperfW2Service, metav1.CreateOptions{}); err != nil {
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

	_, err := c.CoreV1().ReplicationControllers(testNamespace).Create(context.Background(), &api.ReplicationController{
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
	}, metav1.CreateOptions{})
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

		_, err := c.CoreV1().ReplicationControllers(testNamespace).Create(context.Background(), &api.ReplicationController{
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
		}, metav1.CreateOptions{})
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
func getLogsFromPod(c *kubernetes.Clientset, podName string) (*string, error) {
	body, err := c.CoreV1().Pods(testNamespace).GetLogs(podName, &api.PodLogOptions{Timestamps: false}).DoRaw(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error (%s) reading logs from pod %s", err, podName)
	}
	logData := string(body)
	return &logData, nil
}

func getDataFromPod(c *kubernetes.Clientset, podName, startMarker, endMarker string) (*string, error) {
	logData, err := getLogsFromPod(c, podName)
	if err != nil {
		return nil, err
	}
	index := strings.Index(*logData, startMarker)
	endIndex := strings.Index(*logData, endMarker)
	if index == -1 || endIndex == -1 {
		return nil, nil
	}
	data := string((*logData)[index+len(startMarker)+1 : endIndex])
	return &data, nil
}

func processRawData(rawData *string, fileExtension string) error {
	t := time.Now().UTC()
	outputFileDirectory := fmt.Sprintf("results_%s-%s", testNamespace, tag)
	outputFilePrefix := fmt.Sprintf("%s-%s_%s.", testNamespace, tag, t.Format("20060102150405"))
	outputFilePath := fmt.Sprintf("%s/%s%s", outputFileDirectory, outputFilePrefix, fileExtension)
	fmt.Printf("Test concluded - Raw data written to %s\n", outputFilePath)
	if _, err := os.Stat(outputFileDirectory); os.IsNotExist(err) {
		err := os.Mkdir(outputFileDirectory, 0766)
		if err != nil {
			return err
		}
	}
	fd, err := os.OpenFile(outputFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("ERROR writing output datafile: %s", err)
	}
	defer fd.Close()
	_, err = fd.WriteString(*rawData)
	if err != nil {
		return fmt.Errorf("error writing string: %s", err)
	}
	return nil
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
			if pods, err = c.CoreV1().Pods(testNamespace).List(context.Background(), everythingSelector); err != nil {
				fmt.Println("Failed to fetch pods - waiting for pod creation", err)
				continue
			}
			orchestratorPodName = getOrchestratorPodName(pods)
		}
		fmt.Println("Orchestrator Pod is", orchestratorPodName)

		// The pods orchestrate themselves, we just wait for the results file to show up in the orchestrator container
		for {
			// Monitor the orchestrator pod for the CSV results file
			csvdata, err := getDataFromPod(c, orchestratorPodName, csvDataMarker, csvEndDataMarker)
			if err != nil {
				fmt.Println("Error getting CSV data from orchestrator pod", err)
				return false
			}
			if csvdata == nil {
				fmt.Println("Scanned orchestrator pod filesystem - no results file found yet...")
				time.Sleep(60 * time.Second)
				continue
			}

			if jsonOutput {
				jsondata, err := getDataFromPod(c, orchestratorPodName, jsonDataMarker, jsonEndDataMarker)
				if err != nil {
					fmt.Println("Error getting JSON data from orchestrator pod", err)
					return false
				}
				if jsondata == nil {
					fmt.Println("Scanned orchestrator pod filesystem - no json data found yet...")
					time.Sleep(60 * time.Second)
					continue
				}
				err = processRawData(jsondata, "json")
				if err != nil {
					fmt.Println("Error processing JSON data", err)
					return false
				}
			}

			err = processRawData(csvdata, "csv")
			if err != nil {
				fmt.Println("Error processing CSV data", err)
				return false
			}

			break
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
	// cleanup(c)
}

// TODO: Add support for these tests to be utilized as a library
func LaunchNetperfTests(iterations int, kubeConfig string, testNamespace string, netperfImage string, testFrom, testTo int) {
	flag.Set("iterations", fmt.Sprintf("%d", iterations))
	flag.Set("kubeConfig", kubeConfig)
	flag.Set("namespace", testNamespace)
	flag.Set("image", netperfImage)
	flag.Set("testFrom", fmt.Sprintf("%d", testFrom))
	flag.Set("testTo", fmt.Sprintf("%d", testTo))
	main()
}
