package lib

import (
	"fmt"
)

const (
	csvDataMarker     = "GENERATING CSV OUTPUT"
	csvEndDataMarker  = "END CSV DATA"
	jsonDataMarker    = "GENERATING JSON OUTPUT"
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

type Result struct {
	JsonResultFile string
	CsvResultFile  string
}

func PerformTests(testParams TestParams) ([]Result, error) {
	c, err := setupClient(testParams.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %v", err)
	}
	nodes, err := getMinionNodes(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %v", err)
	}
	if len(nodes.Items) < 2 {
		return nil, fmt.Errorf("at least 2 nodes are required to run the tests")
	}
	primaryNode := nodes.Items[0]
	secondaryNode := nodes.Items[1]

	fmt.Println("Primary Node   : ", primaryNode.Name)
	fmt.Println("Secondary Node : ", secondaryNode.Name)

	if testParams.CleanupOnly {
		cleanup(c, testParams.TestNamespace)
		return nil, nil
	}

	fmt.Println("Network Performance Test")
	fmt.Println("Parameters :")
	fmt.Println("Iterations      : ", testParams.Iterations)
	fmt.Println("Test Namespace  : ", testParams.TestNamespace)
	fmt.Println("Docker image    : ", testParams.Image)
	fmt.Println("------------------------------------------------------------")

	results, err := executeTests(c, testParams, primaryNode, secondaryNode)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tests: %v", err)
	}
	cleanup(c, testParams.TestNamespace)
	return results, nil
}
