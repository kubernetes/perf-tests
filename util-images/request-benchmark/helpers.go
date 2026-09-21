/*
Copyright 2025 The Kubernetes Authors.

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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
)

func getConfig() (*rest.Config, error) {
	if _, ok := os.LookupEnv("KUBERNETES_PORT"); ok {
		return rest.InClusterConfig()
	}

	if kubeconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if home, ok := os.LookupEnv("HOME"); ok {
		kubeconfig := filepath.Join(home, ".kube", "config")
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	return nil, fmt.Errorf("could not create client-go config")
}

func tryThrottle(ctx context.Context, rateLimiter flowcontrol.RateLimiter) error {
	if rateLimiter == nil {
		return nil
	}

	now := time.Now()
	err := rateLimiter.Wait(ctx)
	if err != nil {
		err = fmt.Errorf("client rate limiter Wait returned an error: %w", err)
	}
	latency := time.Since(now)

	if latency > time.Second {
		log.Printf("Waited for %v due to client-side throttling, not priority and fairness", latency)
	}

	return err
}
