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
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/metrics"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/utils"
)

const workerCount = 10

// TestClient is an implementation of pod creation reachability latency test
// client.
type TestClient struct {
	*utils.BaseTestClientConfig

	informerStopChan     chan struct{}
	stopInformerOnce     sync.Once
	podInformer          cache.SharedIndexInformer
	podCreationWorkQueue workqueue.TypedInterface[utils.PodEvent]
	// Used to track pods that are received through Add pod event.
	createdPodsMap *utils.BoolMapWithLock
	// Used to track target pods that the client is already trying to reach.
	targetPodsMap *utils.TimeMapWithLock
}

// NewTestClient creates a new test client based on the command flags.
func NewTestClient(stopChan chan os.Signal) (*TestClient, error) {
	baseTestClientConfig, err := utils.CreateBaseTestClientConfig(stopChan)
	if err != nil {
		return nil, err
	}

	testClient := &TestClient{BaseTestClientConfig: baseTestClientConfig}

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
	metrics.RegisterMetrics(metrics.PodIPAddressAssignedLatency, metrics.PodCreationReachabilityLatency)
	defer utils.ShutDownMetricsServer(context.TODO(), c.MetricsServer)

	if err := c.measurePodCreation(); err != nil {
		klog.Warningf("Pod creation measurement is not fully set up, continuing to measure: %v", err)
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
	if err := utils.VerifyHostConfig(c.HostConfig); err != nil {
		return fmt.Errorf("failed to verify host configuration, error: %v", err)
	}

	if err := utils.VerifyTargetConfig(c.TargetConfig); err != nil {
		return fmt.Errorf("failed to verify target configuration, error: %v", err)
	}

	c.InitStopChannel()
	c.podCreationWorkQueue = workqueue.NewTyped[utils.PodEvent]()
	c.informerStopChan = make(chan struct{})
	c.createdPodsMap = &utils.BoolMapWithLock{Mp: make(map[string]bool, c.TargetConfig.MaxTargets)}
	c.targetPodsMap = &utils.TimeMapWithLock{Mp: make(map[string]time.Time, c.TargetConfig.MaxTargets)}

	go func() {
		select {
		case <-c.StopCh:
			c.stopInformerChan()
		case <-c.informerStopChan:
		}
	}()

	return nil
}

// measurePodCreation runs the network policy enforcement latency test for pod
// creation.
func (c *TestClient) measurePodCreation() error {
	klog.Infof("Starting to measure pod creation reachability latency")
	return c.createPodInformer()
}

// createPodInformer creates a WatchList-backed pod informer scoped to the
// target namespace and label selector, enqueuing pod events for the workers.
// Workers start before the informer so that events received right after the
// initial sync are not delayed by the sync poll interval. If the sync times
// out, the informer and workers keep running so that a late sync still
// produces samples.
func (c *TestClient) createPodInformer() error {
	klog.Infof("Creating PodWatcher for namespace %q and labelSelector %q", c.TargetConfig.TargetNamespace, c.TargetConfig.TargetLabelSelector)

	informer := corev1informers.NewFilteredPodInformer(
		c.K8sClient,
		c.TargetConfig.TargetNamespace,
		0,
		cache.Indexers{utils.NameIndex: utils.MetaNameIndexFunc},
		func(options *metav1.ListOptions) {
			options.LabelSelector = c.TargetConfig.TargetLabelSelector
		},
	)
	if err := informer.SetTransform(utils.TrimPod); err != nil {
		return fmt.Errorf("failed to set pod informer transform: %w", err)
	}

	handlePodEvent := func(obj interface{}, isAddEvent bool) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			klog.Warningf("handlePodEvent() failed to convert newObj (%T) to *corev1.Pod", obj)
			return
		}
		c.podCreationWorkQueue.Add(utils.PodEvent{PodName: pod.GetName(), IsAddEvent: isAddEvent})
	}

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handlePodEvent(obj, true)
		},
		UpdateFunc: func(_, newObj interface{}) {
			handlePodEvent(newObj, false)
		},
	})
	if err != nil {
		return err
	}

	c.podInformer = informer
	klog.Infof("Starting %d workers for parallel processing of pod Add and Update events", workerCount)
	for i := 0; i < workerCount; i++ {
		go c.processPodCreationEvents()
	}

	go informer.Run(c.informerStopChan)
	// informerStopChan also closes when MaxTargets is reached, which can happen
	// before HasSynced becomes true or before the sync poll observes it.
	if !utils.WaitForInformerSync(c.informerStopChan, c.InformerSyncTimeout, informer.HasSynced) && !informer.HasSynced() && !c.hasReachedMaxTargets() {
		return fmt.Errorf("failed to sync pod informer")
	}

	return nil
}

func (c *TestClient) hasReachedMaxTargets() bool {
	c.targetPodsMap.Lock.RLock()
	defer c.targetPodsMap.Lock.RUnlock()
	return len(c.targetPodsMap.Mp) >= c.TargetConfig.MaxTargets
}

// processPodCreationEvents is run by each worker to process the incoming pod
// watch events.
func (c *TestClient) processPodCreationEvents() {
	wait.Until(c.processNextPodCreationEvent, 0, c.informerStopChan)
	klog.Infof("processPodCreationEvents() goroutine quitting")
}

// processNextPodCreationEvent processes the next pod watch event in the work
// queue, by sending traffic to the pods that have IP address assigned, until
// the first successful response.
func (c *TestClient) processNextPodCreationEvent() {
	podEvent, quit := c.podCreationWorkQueue.Get()
	if quit {
		return
	}
	defer c.podCreationWorkQueue.Done(podEvent)

	startTime := time.Now()

	// Skip Update pod events for pods that aren't tracked since their creation.
	// Only new pods should be included as targets.
	if c.skipUpdateForUntrackedPods(&podEvent) {
		return
	}

	objList, err := c.podInformer.GetIndexer().ByIndex(utils.NameIndex, podEvent.PodName)
	if err != nil {
		klog.Warningf("Failed to get pod object from the informer's cache, for pod name %q", podEvent.PodName)
		return
	}

	if len(objList) < 1 {
		klog.Warningf("Pod object does not exist in the informer's cache, for pod name %q", podEvent.PodName)
		return
	}

	pod, ok := objList[0].(*corev1.Pod)
	if !ok {
		klog.Warningf("processNextPodCreationEvent() failed to convert obj (%T) to *corev1.Pod", objList[0])
		return
	}

	if len(pod.Status.PodIP) == 0 {
		return
	}
	podName := pod.GetName()
	podCreationTime := pod.GetCreationTimestamp().Time
	var (
		targetUsed        bool
		exceededMax       bool
		reachedMaxTargets bool
	)

	c.targetPodsMap.Lock.Lock()
	_, targetUsed = c.targetPodsMap.Mp[podName]
	if !targetUsed {
		if len(c.targetPodsMap.Mp) >= c.TargetConfig.MaxTargets {
			exceededMax = true
		} else {
			c.targetPodsMap.Mp[podName] = startTime
			reachedMaxTargets = len(c.targetPodsMap.Mp) == c.TargetConfig.MaxTargets
		}
	}
	c.targetPodsMap.Lock.Unlock()

	if targetUsed {
		return
	}

	if exceededMax {
		c.stopInformerChan()
		return
	}

	podAssignedIPLatency := startTime.Sub(podCreationTime)
	metrics.PodIPAddressAssignedLatency.Observe(podAssignedIPLatency.Seconds())
	klog.Infof("Test client got pod %q with assigned IP %q, %v after pod creation", podName, pod.Status.PodIP, podAssignedIPLatency)

	target := &utils.TargetSpec{
		IP:        pod.Status.PodIP,
		Port:      c.TargetConfig.TargetPort,
		Name:      podName,
		Namespace: pod.GetNamespace(),
		StartTime: startTime,
	}
	go utils.RecordFirstSuccessfulRequestUntilStop(target, c.StopCh, c.ProbeFunc, c.reportReachedTime)

	if reachedMaxTargets {
		c.stopInformerChan()
	}
}

// reportReachedTime records network policy enforcement latency for pod creation.
func (c *TestClient) reportReachedTime(target *utils.TargetSpec, reachedTime time.Time) {
	latency := reachedTime.Sub(target.StartTime)
	metrics.PodCreationReachabilityLatency.Observe(latency.Seconds())
	klog.Infof("Pod %q in namespace %q reached %v after pod IP was assigned", target.Name, target.Namespace, latency)
}

// skipUpdateForUntrackedPods tracks Add pod events and skips Update pod events
// for pods that aren't tracked since their creation.
func (c *TestClient) skipUpdateForUntrackedPods(podEvent *utils.PodEvent) bool {
	if podEvent.IsAddEvent {
		c.createdPodsMap.Lock.Lock()
		c.createdPodsMap.Mp[podEvent.PodName] = true
		c.createdPodsMap.Lock.Unlock()
	} else {
		c.createdPodsMap.Lock.RLock()
		_, trackedSinceCreation := c.createdPodsMap.Mp[podEvent.PodName]
		c.createdPodsMap.Lock.RUnlock()

		if !trackedSinceCreation {
			return true
		}
	}

	return false
}

// stopInformerChan stops the pod informer and shuts down the work queue once.
func (c *TestClient) stopInformerChan() {
	c.stopInformerOnce.Do(func() {
		close(c.informerStopChan)
		c.podCreationWorkQueue.ShutDown()
	})
}
