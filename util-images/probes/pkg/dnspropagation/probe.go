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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net"

	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var (
	statefulSet          = flag.String("dns-propagation-probe-stateful-set", "", "Name of the statefulSet workload")
	service              = flag.String("dns-propagation-probe-service", "", "Name of the headless service that exposes the statefulSet resides")
	namespace            = flag.String("dns-propagation-probe-namespace", "default", "The namespace where the statefulSet resides")
	clusterDomain        = flag.String("dns-propagation-probe-cluster-domain", "cluster", "Name of cluster domain where the statefulSet resides")
	suffix               = flag.String("dns-propagation-probe-suffix", "local", "DNS label suffix")
	interval             = flag.Duration("dns-propagation-probe-interval", 100*time.Millisecond, "Interval between DNS lookups")
	podCount             = flag.Int("dns-propagation-probe-pod-count", 0, "Number of pods in the statefulSet")
	sampleCount          = flag.Int("dns-propagation-probe-sample-count", 0, "Number of pods to test dns propagation against in the statefulSet, defaults to min(100, Ceil(SQRT(podCount))")
	enableErrorLogging   = flag.Bool("enable-error-logging", false, "Enable logging for real errors and timestamps.")
	enableLatencyLogging = flag.Bool("enable-latency-logging", false, "Enable logging for latencies timestamps.")
)

var (
	errorLogger   = slog.New(slog.NewJSONHandler(io.Discard, nil))
	latencyLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
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
	if *enableErrorLogging {
		errorLogger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	} else {
		errorLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if *enableLatencyLogging {
		latencyLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	} else {
		latencyLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	// creates the in-cluster config
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Can not build inClusterConfig, error:%v", err)
	}
	kubeConfig.QPS = 100.0
	kubeConfig.Burst = 200

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

	indices := selectSample(*podCount, *sampleCount)
	targetPods := make(map[string]struct{}, len(indices))
	for _, idx := range indices {
		targetPods[fmt.Sprintf("%s-%d", *statefulSet, idx)] = struct{}{}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan DNSPodPropagationResult, *sampleCount)

	durations := make([]float64, 0, *sampleCount)
	collectorDone := make(chan struct{})
	go func() {
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
		close(collectorDone)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := informers.NewSharedInformerFactoryWithOptions(
		client,
		0,
		informers.WithNamespace(*namespace),
	)
	podInformer := factory.Core().V1().Pods().Informer()

	if err := podInformer.SetTransform(transformPod); err != nil {
		klog.Fatalf("Failed to set pod transform: %v", err)
	}

	handlePod := func(obj interface{}) {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			return
		}

		mu.Lock()
		if _, exists := targetPods[pod.Name]; !exists {
			mu.Unlock()
			return
		}

		readyTime, isReady := getPodReadyTransitionTime(pod)
		if !isReady {
			mu.Unlock()
			return
		}

		delete(targetPods, pod.Name)
		remaining := len(targetPods)
		mu.Unlock()

		url := fmt.Sprintf("%s.%s.%s.%s.%s.%s", pod.Name, *service, *namespace, "svc", *clusterDomain, *suffix)

		wg.Add(1)
		go func(url, podName string, readyTime time.Time) {
			defer wg.Done()

			duration := probeDNSUntilResolved(url, readyTime, *interval)
			ch <- DNSPodPropagationResult{
				podName:  podName,
				duration: duration,
			}
		}(url, pod.Name, readyTime)

		if remaining == 0 {
			cancel()
		}
	}

	if _, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: handlePod,
		UpdateFunc: func(oldObj, newObj interface{}) {
			handlePod(newObj)
		},
	}); err != nil {
		klog.Fatalf("Failed to add event handler: %v", err)
	}

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	<-ctx.Done()

	klog.V(2).Infof("Waiting for all sample pods processes to finish")
	wg.Wait()
	close(ch)
	<-collectorDone
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

// transformPod strips spec and unneeded metadata to minimize memory usage under scale testing.
func transformPod(obj interface{}) (interface{}, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return obj, nil
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Status: v1.PodStatus{
			Conditions: pod.Status.Conditions,
		},
	}, nil
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

func probeDNSUntilResolved(url string, readyTimestamp time.Time, interval time.Duration) time.Duration {
	klog.V(4).Infof("Starting dns propagation calculation for pod %s ...", url)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			if err := lookupFunc(url); err != nil {
				continue
			}

			endTime := time.Now()
			duration := endTime.Sub(readyTimestamp)
			klog.V(2).Infof("DNS lookup succeeded for pod %s, timestamp= %v, DNS propagation duration= %v s", url, readyTimestamp, duration)
			latencyLogger.Info("DNS propagation latency recorded",
				"hostname", url,
				"timestamp", time.Now(),
				"propagationLatency (s)", duration.Seconds())
			return duration
		}
	}
}

var lookupFunc = lookup

// lookup performs a DNS lookup for the given URL.
func lookup(url string) error {
	_, err := net.LookupIP(url)
	return err
}

func getPodReadyTransitionTime(pod *v1.Pod) (time.Time, bool) {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return condition.LastTransitionTime.Time, true
		}
	}
	return time.Time{}, false
}
