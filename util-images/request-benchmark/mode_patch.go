/*
Copyright The Kubernetes Authors.

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

package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
)

func runPatch(args []string) error {
	fs := flag.NewFlagSet("patch", flag.ExitOnError)
	klog.InitFlags(fs)

	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig. Uses in-cluster config if empty.")
	namespace := fs.String("namespace", "", "Target namespace to operate in.")
	podCount := fs.Int("pod-count", 0, "Number of distinct pods. Patch will edit pods combine prefix name with random number based on pod count.")
	podPrefix := fs.String("pod-name-prefix", "", "Prefix for target pod names.")
	qps := fs.Float64("qps", 0, "The QPS limit for all requests.")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *qps == 0 {
		return fmt.Errorf("--qps must be > 0")
	}
	if *podCount == 0 {
		return fmt.Errorf("--pod-count must be > 0")
	}
	if *podPrefix == "" {
		return fmt.Errorf("--pod-name-prefix must be non empty")
	}
	if *namespace == "" {
		return fmt.Errorf("--namespace must be non empty")
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		config, err = getConfig()
		if err != nil {
			return fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	}
	config.QPS = float32(*qps)
	config.Burst = int(*qps) + 50

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	rateLimiter := flowcontrol.NewTokenBucketRateLimiter(float32(*qps), 10)

	ctx := context.Background()
	klog.Infof("Starting patch workload: namespace=%q, podCount=%d", *namespace, *podCount)

	for {
		id := rand.Intn(*podCount)
		podName := fmt.Sprintf("%s%d", *podPrefix, id)

		if err := tryThrottle(ctx, rateLimiter); err != nil {
			klog.Warningf("Got error throttling a request: %v", err)
			continue
		}

		go patch(ctx, clientset, *namespace, podName)
	}

}

func patch(ctx context.Context, clientset kubernetes.Interface, namespace, podName string) {
	patchPayload := []byte(fmt.Sprintf(`{"metadata":{"labels":{"bench-updated":"%d"}}}`, time.Now().UnixNano()))
	_, err := clientset.CoreV1().Pods(namespace).Patch(ctx, podName, types.StrategicMergePatchType, patchPayload, metav1.PatchOptions{})
	if err != nil {
		klog.Warningf("Failed to patch pod %s: %v", podName, err)
	}
}
