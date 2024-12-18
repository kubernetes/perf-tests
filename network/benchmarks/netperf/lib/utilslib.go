package lib

import (
	"context"
	"fmt"
	"time"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var everythingSelector metav1.ListOptions = metav1.ListOptions{}

func setupClient(kubeConfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return clientset, nil
}

func getMinionNodes(c *kubernetes.Clientset) (*api.NodeList, error) {
	nodes, err := c.CoreV1().Nodes().List(
		context.Background(),
		metav1.ListOptions{
			FieldSelector: "spec.unschedulable=false",
			// for now the tests can only run on linux/amd64 nodes
			LabelSelector: "kubernetes.io/os=linux,kubernetes.io/arch=amd64",
		})
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %v", err)
	}
	return nodes, nil
}

func createServices(c *kubernetes.Clientset, testNamespace string) error {
	if _, err := c.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{}); err != nil {
		_, err := c.CoreV1().Namespaces().Create(context.Background(), &api.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %s: %v", testNamespace, err)
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
		return fmt.Errorf("failed to create orchestrator service: %v", err)
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
		return fmt.Errorf("failed to create netperf-w2 service: %v", err)
	}
	fmt.Println("Created netperf-w2 service")
	return nil
}

func createRCs(c *kubernetes.Clientset, testParams TestParams, primaryNode, secondaryNode api.Node) error {
	// Create the orchestrator RC
	name := "netperf-orch"
	fmt.Println("Creating replication controller", name)
	replicas := int32(1)

	_, err := c.CoreV1().ReplicationControllers(testParams.TestNamespace).Create(context.Background(), &api.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: api.ReplicationControllerSpec{
			Replicas: &replicas,
			Selector: map[string]string{"app": name},
			Template: &api.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: api.PodSpec{
					NodeSelector: map[string]string{"kubernetes.io/os": "linux", "kubernetes.io/arch": "amd64"},
					Containers: []api.Container{
						{
							Name:  name,
							Image: testParams.Image,
							Ports: []api.ContainerPort{{ContainerPort: orchestratorPort}},
							Args: []string{
								"--mode=orchestrator",
								fmt.Sprintf("--testFrom=%d", testParams.TestFrom),
								fmt.Sprintf("--testTo=%d", testParams.TestTo),
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
		return fmt.Errorf("error creating orchestrator replication controller %s: %v", name, err)
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

		_, err := c.CoreV1().ReplicationControllers(testParams.TestNamespace).Create(context.Background(), &api.ReplicationController{
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
								Name:  name,
								Image: testParams.Image,
								Ports: portSpec,
								Args: []string{
									"--mode=worker",
									fmt.Sprintf("--testFrom=%d", testParams.TestFrom),
									fmt.Sprintf("--testTo=%d", testParams.TestTo),
								},
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
			return fmt.Errorf("error creating worker replication controller %s: %v", name, err)
		}
	}

	return nil
}

func executeTests(c *kubernetes.Clientset, testParams TestParams, primaryNode, secondaryNode api.Node) ([]Result, error) {
	results := make([]Result, testParams.Iterations)
	for i := 0; i < testParams.Iterations; i++ {
		cleanup(c, testParams.TestNamespace)
		if err := createServices(c, testParams.TestNamespace); err != nil {
			return nil, fmt.Errorf("failed to create services: %v", err)
		}
		time.Sleep(3 * time.Second)
		if err := createRCs(c, testParams, primaryNode, secondaryNode); err != nil {
			return nil, fmt.Errorf("failed to create replication controllers: %v", err)
		}
		fmt.Println("Waiting for netperf pods to start up")

		orchestratorPodName, err := getOrchestratorPodName(c, testParams.TestNamespace, 3*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("failed to get orchestrator pod name: %v", err)
		}
		fmt.Println("Orchestrator Pod is", orchestratorPodName)

		var jsonFilePath string
		var csvFilePath string

		// The pods orchestrate themselves, we just wait for the results file to show up in the orchestrator container
		for {
			// Monitor the orchestrator pod for the CSV results file
			csvdata, err := getDataFromPod(c, orchestratorPodName, csvDataMarker, csvEndDataMarker, testParams.TestNamespace)
			if err != nil {
				return nil, fmt.Errorf("error getting CSV data from orchestrator pod: %v", err)
			}
			if csvdata == nil {
				fmt.Println("Scanned orchestrator pod filesystem - no results file found yet...")
				time.Sleep(60 * time.Second)
				continue
			}

			if testParams.JsonOutput {
				jsondata, err := getDataFromPod(c, orchestratorPodName, jsonDataMarker, jsonEndDataMarker, testParams.TestNamespace)
				if err != nil {
					return nil, fmt.Errorf("error getting JSON data from orchestrator pod: %v", err)
				}
				if jsondata == nil {
					fmt.Println("Scanned orchestrator pod filesystem - no json data found yet...")
					time.Sleep(60 * time.Second)
					continue
				}
				jsonFilePath, err = processRawData(jsondata, testParams.TestNamespace, testParams.Tag, "json")
				if err != nil {
					return nil, fmt.Errorf("error processing JSON data: %v", err)
				}
			}

			csvFilePath, err = processRawData(csvdata, testParams.TestNamespace, testParams.Tag, "csv")
			if err != nil {
				return nil, fmt.Errorf("error processing CSV data: %v", err)
			}

			break
		}
		fmt.Printf("TEST RUN (Iteration %d) FINISHED - cleaning up services and pods\n", i)
		results[i] = Result{JsonResultFile: jsonFilePath, CsvResultFile: csvFilePath}
	}
	return results, nil
}

func getOrchestratorPodName(c *kubernetes.Clientset, testNamespace string, timeout time.Duration) (string, error) {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Println("Waiting for orchestrator pod creation")
			pods, err := c.CoreV1().Pods(testNamespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=netperf-orch",
			})
			if err != nil {
				fmt.Println("Failed to fetch pods - waiting for pod creation", err)
				continue
			}
			if len(pods.Items) == 0 {
				fmt.Println("No orchestrator pods found yet")
				continue
			}

			pod := pods.Items[0]
			podStatus := pod.Status

			if podStatus.Phase == api.PodRunning {
				return pod.GetName(), nil
			}

			for _, containerStatus := range podStatus.ContainerStatuses {
				if waiting := containerStatus.State.Waiting; waiting != nil {
					switch waiting.Reason {
					case "ErrImagePull", "CrashLoopBackOff", "ImagePullBackOff":
						return "", fmt.Errorf("orchestrator pod error: %s - %v", waiting.Reason, waiting.Message)
					}
				}
			}
			fmt.Println("Orchestrator pod is not running yet")
		case <-timeoutCh:
			return "", fmt.Errorf("timed out waiting for orchestrator pod to be created")
		}
	}
}

func cleanup(c *kubernetes.Clientset, testNamespace string) {
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
