package lib

import (
	"fmt"
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

type TestParams struct {
	Iterations    int
	Tag           string
	TestNamespace string
	Image         string
	CleanupOnly   bool
	TestFrom      int
	TestTo        int
	JsonOutput    bool
	KubeConfig    string
}

func PerformTests(testParams TestParams) error {
	c, err := setupClient(testParams.KubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %v", err)
	}
	nodes, err := getMinionNodes(c)
	if err != nil {
		return fmt.Errorf("failed to get nodes: %v", err)
	}
	if len(nodes.Items) < 2 {
		return fmt.Errorf("at least 2 nodes are required to run the tests")
	}
	primaryNode := nodes.Items[0]
	secondaryNode := nodes.Items[1]

	if testParams.CleanupOnly {
		cleanup(c, testParams.TestNamespace)
		return nil
	}

	fmt.Println("Network Performance Test")
	fmt.Println("Parameters :")
	fmt.Println("Iterations      : ", testParams.Iterations)
	fmt.Println("Test Namespace  : ", testParams.TestNamespace)
	fmt.Println("Docker image    : ", testParams.Image)
	fmt.Println("------------------------------------------------------------")

	if err := executeTests(c, testParams, primaryNode, secondaryNode); err != nil {
		return fmt.Errorf("failed to execute tests: %v", err)
	}
	cleanup(c, testParams.TestNamespace)
	return nil
}
