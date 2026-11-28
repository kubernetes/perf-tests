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
	"cmp"
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1informers "k8s.io/client-go/informers/core/v1"
	networkingv1informers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/metrics"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/utils"
)

const defaultAllowPolicyWaitTimeout = 30 * time.Second

// TestClient is an implementation of policy creation enforcement latency test
// client.
type TestClient struct {
	*utils.BaseTestClientConfig

	// The name of the egress policy that allows traffic from test client pods to
	// target pods.
	allowPolicyName         string
	allowPolicyWaitTimeout  time.Duration
	allowPolicyReadyCh      chan struct{}
	allowPolicyReadyOnce    sync.Once
	allowPolicyCreationTime time.Time

	podInformer    cache.SharedIndexInformer
	policyInformer cache.SharedIndexInformer
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
	c.MetricsServer = metrics.StartMetricsServer(fmt.Sprintf(":%d", c.HostConfig.MetricsPort))
	metrics.RegisterMetrics(metrics.PolicyEnforceLatencyPolicyCreation)
	defer utils.ShutDownMetricsServer(context.TODO(), c.MetricsServer)

	if err := c.measureNetPolicyCreation(); err != nil {
		klog.Errorf("Policy creation enforcement latency test failed, error: %v", err)
	}
	// Prevent application from terminating and pod from restarting.
	// The test will run only once when the application is deployed.
	// The pod needs to be recreated to rerun the test.
	c.WaitUntilStopped()
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

	c.InitStopChannel()
	c.allowPolicyReadyCh = make(chan struct{})
	if c.allowPolicyWaitTimeout <= 0 {
		c.allowPolicyWaitTimeout = defaultAllowPolicyWaitTimeout
	}
	return nil
}

// measureNetPolicyCreation runs the network policy enforcement latency test for
// network policy creation.
func (c *TestClient) measureNetPolicyCreation() error {
	klog.Infof("Starting to measure network policy enforcement latency on network policy creation")
	podStopCh, stopPodInformer := c.newInformerStopChannel()
	defer stopPodInformer()
	policyStopCh, stopPolicyInformer := c.newInformerStopChannel()
	defer stopPolicyInformer()

	if err := c.startAndSyncInformers(podStopCh, policyStopCh); err != nil {
		return err
	}

	podList, err := c.listTargetPods()
	// The target list is fixed from here on, so the pod watch is not needed.
	stopPodInformer()
	if err != nil {
		return fmt.Errorf("failed to get the pod list, error: %v", err)
	}
	klog.Infof("%d pods listed", len(podList))

	var wg sync.WaitGroup

	// Keep sending requests to all pods until all of them are reached.
	for _, pod := range podList {
		target := &utils.TargetSpec{
			IP:        pod.Status.PodIP,
			Port:      c.TargetConfig.TargetPort,
			Name:      pod.GetName(),
			Namespace: pod.GetNamespace(),
		}

		wg.Add(1)
		go func(target *utils.TargetSpec) {
			defer wg.Done()
			c.probeUntilReached(target)
		}(target)
	}

	wg.Wait()
	return nil
}

// probeUntilReached probes target until the first success and reports the
// latency only if an earlier probe failed, which shows that traffic was denied
// before the allow policy took effect.
func (c *TestClient) probeUntilReached(target *utils.TargetSpec) {
	if target == nil || len(target.IP) == 0 {
		klog.Warningf("Skipping a target for policy enforcement latency. Target needs to have IP address and port specified. Target: %v", target)
		return
	}
	probe := c.ProbeFunc
	if probe == nil {
		probe = utils.NewCurlProbe(target)
	}
	var denied atomic.Bool
	countingProbe := func(t *utils.TargetSpec) error {
		err := probe(t)
		if err != nil {
			denied.Store(true)
		}
		return err
	}
	utils.RecordFirstSuccessfulRequestUntilStop(target, c.StopCh, countingProbe, func(t *utils.TargetSpec, reachedTime time.Time) {
		if !denied.Load() {
			klog.Warningf("First request to pod %q in namespace %q with IP %q succeeded, so traffic was not denied before the allow policy; dropping the sample", t.Name, t.Namespace, t.IP)
			return
		}
		c.reportReachedTimeForPolicyCreation(t, reachedTime)
	})
}

// newInformerStopChannel returns a channel that closes when the returned stop
// function is called or when StopCh closes. Informers use it instead of StopCh
// so that they can stop without ending WaitUntilStopped.
func (c *TestClient) newInformerStopChannel() (<-chan struct{}, func()) {
	stopCh := make(chan struct{})
	stop := sync.OnceFunc(func() { close(stopCh) })
	go func() {
		select {
		case <-c.StopCh:
			stop()
		case <-stopCh:
		}
	}()
	return stopCh, stop
}

// startAndSyncInformers starts WatchList-backed Pod and NetworkPolicy informers
// that run until their stop channels close.
func (c *TestClient) startAndSyncInformers(podStopCh, policyStopCh <-chan struct{}) error {
	podInformer := corev1informers.NewFilteredPodInformer(
		c.K8sClient,
		c.TargetConfig.TargetNamespace,
		0,
		make(cache.Indexers, 0),
		func(options *metav1.ListOptions) {
			options.LabelSelector = c.TargetConfig.TargetLabelSelector
		},
	)
	if err := podInformer.SetTransform(utils.TrimPod); err != nil {
		return fmt.Errorf("failed to set pod informer transform: %w", err)
	}

	allowPolicyFieldSelector := fields.OneTermEqualSelector("metadata.name", c.allowPolicyName).String()
	policyInformer := networkingv1informers.NewFilteredNetworkPolicyInformer(
		c.K8sClient,
		c.HostConfig.HostNamespace,
		0,
		make(cache.Indexers, 0),
		func(options *metav1.ListOptions) {
			options.FieldSelector = allowPolicyFieldSelector
		},
	)
	if err := policyInformer.SetTransform(utils.TrimNetworkPolicy); err != nil {
		return fmt.Errorf("failed to set networkpolicy informer transform: %w", err)
	}
	if _, err := policyInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.recordAllowPolicy,
		UpdateFunc: func(_, newObj interface{}) { c.recordAllowPolicy(newObj) },
	}); err != nil {
		return fmt.Errorf("failed to register networkpolicy event handler: %w", err)
	}

	c.podInformer = podInformer
	c.policyInformer = policyInformer

	go c.policyInformer.Run(policyStopCh)
	go c.podInformer.Run(podStopCh)

	if !utils.WaitForInformerSync(c.StopCh, c.InformerSyncTimeout, c.policyInformer.HasSynced, c.podInformer.HasSynced) {
		return fmt.Errorf("failed to sync pod and networkpolicy informers")
	}
	return nil
}

func (c *TestClient) recordAllowPolicy(obj interface{}) {
	np, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		return
	}
	if np.Namespace != c.HostConfig.HostNamespace || np.Name != c.allowPolicyName {
		return
	}
	// Assigning allowPolicyCreationTime before closing allowPolicyReadyCh
	// publishes the timestamp without locks to all readers waking from <-allowPolicyReadyCh.
	c.allowPolicyReadyOnce.Do(func() {
		c.allowPolicyCreationTime = np.GetCreationTimestamp().Time
		close(c.allowPolicyReadyCh)
	})
}

// listTargetPods returns target pods from the synced pod informer store sorted
// by name and capped to MaxTargets.
func (c *TestClient) listTargetPods() ([]*corev1.Pod, error) {
	items := c.podInformer.GetStore().List()
	if len(items) == 0 {
		return nil, fmt.Errorf("no pods listed for selector %q in namespace %q", c.TargetConfig.TargetLabelSelector, c.TargetConfig.TargetNamespace)
	}

	pods := make([]*corev1.Pod, 0, len(items))
	for _, item := range items {
		pod, ok := item.(*corev1.Pod)
		if !ok {
			continue
		}
		pods = append(pods, pod)
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no pods listed for selector %q in namespace %q", c.TargetConfig.TargetLabelSelector, c.TargetConfig.TargetNamespace)
	}

	slices.SortFunc(pods, func(a, b *corev1.Pod) int {
		return cmp.Compare(a.Name, b.Name)
	})
	if len(pods) > c.TargetConfig.MaxTargets {
		pods = pods[:c.TargetConfig.MaxTargets]
	}

	return pods, nil
}

// reportReachedTimeForPolicyCreation records network policy enforcement latency
// once the allow policy's creation timestamp has arrived over the watch stream.
func (c *TestClient) reportReachedTimeForPolicyCreation(target *utils.TargetSpec, reachedTime time.Time) {
	if len(c.allowPolicyName) == 0 {
		return
	}

	select {
	case <-c.allowPolicyReadyCh:
	default:
		// Dataplane policy programming can let a probe succeed briefly before this
		// pod's own watch receives the ADDED event; wait on the watch rather than
		// dropping the already-captured reachedTime or issuing a GET request.
		timer := time.NewTimer(c.allowPolicyWaitTimeout)
		defer timer.Stop()
		select {
		case <-c.allowPolicyReadyCh:
		case <-c.StopCh:
			return
		case <-timer.C:
			klog.Warningf("Timed out after %v waiting for network policy %s/%s in watch cache when pod %q in namespace %q with IP %q became reachable", c.allowPolicyWaitTimeout, c.HostConfig.HostNamespace, c.allowPolicyName, target.Name, target.Namespace, target.IP)
			return
		}
	}

	latency := reachedTime.Sub(c.allowPolicyCreationTime)
	if latency < 0 {
		klog.Warningf("Pod %q in namespace %q with IP %q was reached %v before network policy %s/%s was created; dropping the sample", target.Name, target.Namespace, target.IP, -latency, c.HostConfig.HostNamespace, c.allowPolicyName)
		return
	}
	metrics.PolicyEnforceLatencyPolicyCreation.Observe(latency.Seconds())
	klog.Infof("Pod %q in namespace %q with IP %q reached %v after policy creation", target.Name, target.Namespace, target.IP, latency)
}
