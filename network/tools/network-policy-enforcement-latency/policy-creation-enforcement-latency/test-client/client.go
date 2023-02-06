/*
Copyright 2023 The Kubernetes Authors.

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

package client

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/metrics"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/utils"
)

// TestClient is an implementation of pod creation reachability latency test
// client.
type TestClient struct {
	*utils.BaseTestClientConfig

	// The name of the egress policy that allows traffic from test client pods to
	// target pods.
	allowPolicyName string
	// Used to cache policy creation time instead of getting it from API server on
	// every successful request.
	policyCreatedTime *utils.TimeWithLock
}

// NewTestClient creates a new test client based on the command flags.
func NewTestClient(stopChan chan os.Signal) (*TestClient, error) {
	var allowPolicyName string
	flag.StringVar(&allowPolicyName, "AllowPolicyName", "", "The name of the egress policy that allows traffic from test client pods to target pods")
	baseTestClientConfig, err := utils.CreateBaseTestClientConfig(stopChan)
	if err != nil {
		return nil, err
	}

	testClient := &TestClient{
		BaseTestClientConfig: baseTestClientConfig,
		allowPolicyName:      allowPolicyName,
	}

	err = testClient.initialize()
	if err != nil {
		return nil, err
	}

	return testClient, nil
}

// Run verifies the test client config, starts the metrics server, and runs the
// network policy enforcement latency test based on the config.
func (c *TestClient) Run() {
	if err := c.measureNetPolicyCreation(); err != nil {
		klog.Errorf("Policy creation enforcement latency test failed, error: %v", err)
	}
	// Prevent application from terminating and pod from restarting.
	// The test will run only once when the application is deployed.
	// The pod needs to be recreated to rerun the test.
	utils.EnterIdleState(c.MainStopChan)
}

// initialize verifies the config and instantiates the objects required for the
// test to run.
func (c *TestClient) initialize() error {
	klog.Infof("Verifying test client configuration")
	if len(c.allowPolicyName) == 0 {
		return fmt.Errorf("AllowPolicyName is not specified for policy creation test")
	}

	if err := utils.VerifyHostConfig(c.HostConfig); err != nil {
		return fmt.Errorf("failed to verify host configuration, error: %v", err)
	}

	if err := utils.VerifyTargetConfig(c.TargetConfig); err != nil {
		return fmt.Errorf("failed to verify target configuration, error: %v", err)
	}

	metrics.RegisterHistogramMetric(metrics.PolicyEnforceLatencyPolicyCreation)
	defer utils.ShutDownMetricsServer(context.TODO(), c.MetricsServer)

	c.policyCreatedTime = &utils.TimeWithLock{Lock: sync.RWMutex{}}

	return nil
}

// measureNetPolicyCreation runs the network policy enforcement latency test for
// network policy creation.
func (c *TestClient) measureNetPolicyCreation() error {
	klog.Infof("Starting to measure network policy enforcement latency on network policy creation")
	podList, err := c.listTargetPods()
	if err != nil {
		return fmt.Errorf("failed to get the pod list, error: %v", err)
	}
	klog.Infof("%d pods listed", len(podList))

	wg := sync.WaitGroup{}

	// Keep sending requests to all pods until all of them are reached.
	for _, pod := range podList {
		target := &utils.TargetSpec{
			IP:        pod.Status.PodIP,
			Port:      c.TargetConfig.TargetPort,
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
		}

		wg.Add(1)
		go func() {
			utils.RecordFirstSuccessfulRequest(target, c.MainStopChan, c.reportReachedTimeForPolicyCreation)
			wg.Done()
		}()
	}

	wg.Wait()
	return nil
}

// listTargetPods returns a list of pods based on the test client config, for
// target namespace and target label selector, with a limit to the listed pods.
func (c *TestClient) listTargetPods() ([]corev1.Pod, error) {
	podList, err := c.K8sClient.CoreV1().Pods(c.TargetConfig.TargetNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: c.TargetConfig.TargetLabelSelector, Limit: int64(c.TargetConfig.MaxTargets)})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods for selector %q in namespace %q, error: %v", c.TargetConfig.TargetLabelSelector, c.TargetConfig.TargetNamespace, err)
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods listed for selector %q in namespace %q, error: %v", c.TargetConfig.TargetLabelSelector, c.TargetConfig.TargetNamespace, err)
	}

	return podList.Items, nil
}

// reportReachedTimeForPolicyCreation records network policy enforcement latency
// metric for network policy creation.
func (c *TestClient) reportReachedTimeForPolicyCreation(target *utils.TargetSpec, reachedTime time.Time) {
	if len(c.allowPolicyName) == 0 {
		return
	}

	var policyCreateTime time.Time
	failed := false

	c.policyCreatedTime.Lock.Lock()

	if c.policyCreatedTime.Time == nil {
		networkPolicy, err := c.K8sClient.NetworkingV1().NetworkPolicies(c.HostConfig.HostNamespace).Get(context.TODO(), c.allowPolicyName, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to get network policies for pod %q in namespace %q with IP %q: %v", target.Name, c.HostConfig.HostNamespace, target.IP, err)
			failed = true
		} else {
			policyCreateTime = networkPolicy.GetCreationTimestamp().Time
			c.policyCreatedTime.Time = &policyCreateTime
		}
	} else {
		policyCreateTime = *c.policyCreatedTime.Time
	}

	c.policyCreatedTime.Lock.Unlock()
	if failed {
		return
	}

	latency := reachedTime.Sub(policyCreateTime)
	metrics.PolicyEnforceLatencyPolicyCreation.Observe(latency.Seconds())
	klog.Infof("Pod %q in namespace %q with IP %q reached %v after policy creation", target.Name, target.Namespace, target.IP, latency)
}
