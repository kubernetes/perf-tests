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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	kubeconfig                  string
	targetNamespace             string
	informerCount               int
	testTimeout                 time.Duration
	enableWatchListAlphaFeature bool
	apiVersion                  string
	resource                    string
)

func main() {
	registerFlags()
	flag.Parse()

	klog.Info("The test binary started with the following arguments:")
	flag.VisitAll(func(f *flag.Flag) {
		klog.Infof("  -%s=%v (%s)\n", f.Name, f.Value, f.Usage)
	})

	if err := os.Setenv("KUBE_FEATURE_WatchListClient", strconv.FormatBool(enableWatchListAlphaFeature)); err != nil {
		klog.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}
	config.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	config.ContentType = "application/vnd.kubernetes.protobuf"
	klog.Infof("The following Kubernetes client config will be used\n%v", config.String())

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		klog.Fatalf("failed to parse api-version %q: %v", apiVersion, err)
	}
	targetGVR := gv.WithResource(resource)

	err = wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		ts := time.Now()
		ctxInformer, cancelInformers := context.WithCancel(ctx)
		defer cancelInformers()

		klog.Infof("Starting %d informers for gvr = %v, targetNamespace = %q", informerCount, targetGVR, targetNamespace)
		informersSynced, err := startInformersForResource(ctxInformer, client, targetGVR, informerCount, targetNamespace)
		if err != nil {
			return false, err
		}

		klog.Infof("Waiting for gvr = %v informers to synced", targetGVR)
		if ok := cache.WaitForCacheSync(ctx.Done(), informersSynced...); !ok {
			return false, fmt.Errorf("timed out waiting for gvr %v informers to sync", targetGVR)
		}
		klog.Infof("All %v informers for gvr = %v synced, time needed = %v", len(informersSynced), targetGVR, time.Now().Sub(ts))
		return false, nil
	})
	if err != nil && !isContextDoneErr(err) {
		klog.Fatal(err)
	}
	klog.Info("Exiting the test app")
}

func registerFlags() {
	klog.InitFlags(flag.CommandLine)

	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig.")
	flag.StringVar(&targetNamespace, "namespace", "", "namespace to run informers for. If empty will open on all namespaces.")
	flag.IntVar(&informerCount, "count", 4, "the number of informers per targetNamespace to run. If empty a default (4) value will be used.")
	flag.DurationVar(&testTimeout, "timeout", time.Minute, "timeout duration for the test")
	flag.BoolVar(&enableWatchListAlphaFeature, "enableWatchListFeature", false, "whether to set KUBE_FEATURE_WatchListClient env var")
	flag.StringVar(&apiVersion, "api-version", "v1", "apiVersion of the target resource (e.g. v1, apps/v1). If empty a default (v1) value will be used.")
	flag.StringVar(&resource, "resource", "secrets", "resource name of the target resource (e.g. pods, deployments). If empty a default (secrets) value will be used.")
}

func startInformersForResource(ctx context.Context, client kubernetes.Interface, gvr schema.GroupVersionResource, count int, namespace string) ([]cache.InformerSynced, error) {
	var informersSynced []cache.InformerSynced
	factories := make([]informers.SharedInformerFactory, 0, count)

	for i := 0; i < count; i++ {
		opts := []informers.SharedInformerOption{}
		if namespace != "" {
			opts = append(opts, informers.WithNamespace(namespace))
		}
		factory := informers.NewSharedInformerFactoryWithOptions(
			client,
			0,
			opts...,
		)
		inf, err := factory.ForResource(gvr)
		if err != nil {
			return nil, err
		}
		informersSynced = append(informersSynced, inf.Informer().HasSynced)
		factories = append(factories, factory)
	}

	for _, factory := range factories {
		factory.Start(ctx.Done())
	}
	return informersSynced, nil
}

func isContextDoneErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
