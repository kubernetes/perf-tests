/*
Copyright 2024 The Kubernetes Authors.

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

package dnspropagation

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

var (
	statefulSet   = flag.String("dns-propagation-probe-stateful-set", "", "Name of the statefulSet workload")
	service       = flag.String("dns-propagation-probe-service", "", "Name of the headless service that exposes the statefulSet resides")
	namespace     = flag.String("dns-propagation-probe-namespace", "default", "The namespace where the statefulSet resides")
	clusterDomain = flag.String("dns-propagation-probe-cluster-domain", "cluster", "Name of cluster domain where the statefulSet resides")
	suffix        = flag.String("dns-propagation-probe-suffix", "local", "DNS label suffix")
	interval      = flag.Duration("dns-propagation-probe-interval", 100*time.Millisecond, "Interval between DNS lookups")
	podCount      = flag.Int("dns-propagation-probe-pod-count", 0, "Number of pods in the statefulSet")
	sampleCount   = flag.Int("dns-propagation-probe-sample-count", 0, "Number of pods to test dns propagation against in the statefulSet, defaults to min(100, Ceil(SQRT(podCount))")
)

type DNSPodPropagationResult struct {
	podName  string
	duration time.Duration
}

// Run is the entry function for the probe.
func Run() {
	if *statefulSet == "" {
		klog.Fatal("--dns-propagation-probe-stateful-set has not been set")
	}
	if *service == "" {
		klog.Fatal("--dns-propagation-probe-service-set has not been set")
	}
	if *podCount <= 0 {
		klog.Fatal("--dns-propagation-probe-pod-count has not been set or is not a positive number")
	}
	if *sampleCount <= 0 {
		f := int(math.Ceil(math.Sqrt(float64(*podCount))))
		f = int(math.Min(float64(f), 100))
		sampleCount = &f
		klog.Warningf("dns-propagation-probe-sample-count not set, defaulting to min(100, Ceil(SQRT(%v))= %v", *podCount, *sampleCount)
	}
	// creates the in-cluster config
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Can not build inClusterConfig, error:%v", err)
	}
	// creates the inCluster kube client
	clientset, err := kubernetes.NewForConfig(kubeConfig)

	if err != nil {
		klog.Fatalf("Failed to build kubeClient, error:%v", err)
	}
	//TODO deprecated as of Go 1.20. To remove when go version gets bumped
	rand.Seed(time.Now().UnixNano())
	runProbe(clientset)
	for {
		klog.V(2).Infof("dns propagation probe complete, waiting until the test finishes...")
		time.Sleep(60 * time.Second)
	}
}

// runProbe runs the DNS propagation probe.
func runProbe(client kubernetes.Interface) {
	klog.V(1).Infof("DNS propagation probe started")
	var wg sync.WaitGroup
	ch := make(chan DNSPodPropagationResult, *sampleCount)
	indices := selectSample(*podCount, *sampleCount)
	durations := make([]float64, 0, *sampleCount)
	for _, idx := range indices {
		podName := fmt.Sprintf("%s-%d", *statefulSet, idx)
		url := fmt.Sprintf("%s.%s.%s.%s.%s.%s", podName, *service, *namespace, "svc", *clusterDomain, *suffix)
		wg.Add(1)
		go func(client kubernetes.Interface, url, podName string, namespace string, interval time.Duration) {
			defer wg.Done()
			result := runSinglePod(client, url, podName, namespace, interval)
			ch <- DNSPodPropagationResult{
				podName:  podName,
				duration: result,
			}
		}(client, url, podName, *namespace, *interval)
	}
	klog.V(2).Infof("Waiting for all sample pods processes to finish")
	go func() {
		wg.Wait()
		close(ch)
	}()

	for propagationResult := range ch {
		labels := prometheus.Labels{
			"namespace": *namespace,
			"service":   *service,
			"podName":   propagationResult.podName,
		}
		DNSPropagationSeconds.With(labels).Set(propagationResult.duration.Seconds())
		DNSPropagationCount.With(labels).Inc()
		durations = append(durations, propagationResult.duration.Seconds())
	}
	klog.V(2).Infof("Finished calculating DNS propagation for all sample pods")

	if len(durations) == 0 {
		klog.Warningf("DNS propagation probe has zero observations")
		return
	}
	sum := 0.0
	for _, duration := range durations {
		sum += duration
	}
	klog.V(1).Infof("DNS propagation probe finished, total of %v observations, average duration, %v s", len(durations), sum/float64(len(durations)))
}

// selectSample returns a slice of indices of length sampleCount, randomly selected from the range [0, podCount).
func selectSample(podCount int, sampleCount int) []int {
	indices := make([]int, podCount)
	for idx := range indices {
		indices[idx] = idx
	}
	rand.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
	indices = indices[:sampleCount]
	return indices
}

// runSinglePod runs a single DNS propagation test for the given pod.
// It returns the duration between the time the DNS lookup succeeds and the time the pod was created.
func runSinglePod(client kubernetes.Interface, url string, podName string, namespace string, interval time.Duration) time.Duration {
	klog.V(4).Infof("Starting dns propagation calculation for pod %s ...", url)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			klog.V(4).Infof("DNS lookup %s", url)
			if err := lookup(url); err != nil {
				klog.Warningf("DNS lookup error: %v", err)
				continue
			}
			endTime := time.Now()
			klog.V(2).Infof("DNS lookup finished for pod %s, finding pod running time...", url)
			timestamp, err := fetchPodRunningTime(client, podName, namespace)
			if err != nil {
				klog.Fatalf("K8s error: %v", err)
				continue
			}
			duration := endTime.Sub(timestamp)
			klog.V(2).Infof("Pod running time fetched for pod %s, timestamp= %v, DNS propagation duration= %v s", url, timestamp, duration)
			return duration
		}
	}
}

// lookup performs a DNS lookup for the given URL.
// It returns nil if the lookup succeeds, or an error otherwise.
func lookup(url string) error {
	_, err := net.LookupIP(url)
	if err != nil {
		return err
	}
	return nil
}

// fetchPodRunningTime fetches the running time of the given pod.
// It retries 3 times with 1 second intervals between retries.
// It returns the running time of the pod, or an error if the pod is not found or if the running time cannot be fetched.
func fetchPodRunningTime(client kubernetes.Interface, podName string, namespace string) (time.Time, error) {
	trials := 0
	for trials < 3 {
		readyTimestamp, err := getPodRunningTimeFromClient(client, podName, namespace)
		if err == nil {
			return readyTimestamp, nil
		}
		klog.Warningf("Failed at obtaining pod running time for pod %s, reason: %v", podName, err)
		trials++
		time.Sleep(1 * time.Second)
	}
	return getPodRunningTimeFromClient(client, podName, namespace)
}

// getPodRunningTimeFromClient fetches the running time of the given pod.
// It returns the running time of the pod, or an error if the pod is not found or if the running time cannot be fetched.
func getPodRunningTimeFromClient(client kubernetes.Interface, podName string, namespace string) (time.Time, error) {
	options := metav1.GetOptions{ResourceVersion: "0"}
	pod, err := client.CoreV1().Pods(namespace).Get(context.TODO(), podName, options)
	if err != nil {
		return time.Now(), err
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			readyTimestamp := condition.LastTransitionTime.Time
			return readyTimestamp, nil
		}
	}
	return time.Now(), errors.New("Ready status wasn't found")
}
