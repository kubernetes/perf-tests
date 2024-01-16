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
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	v1 "k8s.io/client-go/informers/core/v1"
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
)

func main() {
	registerFlags()
	flag.Parse()

	klog.Info("The test binary started with the following arguments:")
	flag.VisitAll(func(f *flag.Flag) {
		klog.Infof("  -%s=%v (%s)\n", f.Name, f.Value, f.Usage)
	})

	if enableWatchListAlphaFeature {
		os.Setenv("KUBE_FEATURE_WatchListClient", "true")
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

	wait.UntilWithContext(ctx, func(ctx context.Context) {
		ts := time.Now()
		ctxInformer, cancelInformers := context.WithCancel(ctx)
		defer cancelInformers()

		klog.Infof("Starting %d secret informers for targetNamespace = %s", informerCount, targetNamespace)
		informersSynced := startInformersFor(ctxInformer, client, informerCount, targetNamespace)

		klog.Info("Waiting for the secrets informers to synced")
		cache.WaitForCacheSync(ctx.Done(), informersSynced...)
		klog.Infof("All %v secret informers synced, time needed = %v", len(informersSynced), time.Now().Sub(ts))
	}, 5*time.Second)

	klog.Info("Exiting the test app")
}

func registerFlags() {
	klog.InitFlags(flag.CommandLine)

	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig.")
	flag.StringVar(&targetNamespace, "namespace", "huge-secrets-1", "namespace that host secrets to list. If empty a default (huge-secrets-1) value will be used.")
	flag.IntVar(&informerCount, "count", 4, "the number of informers per targetNamespace to run. If empty a default (4) value will be used.")
	flag.DurationVar(&testTimeout, "timeout", time.Minute, "timeout duration for the test")
	flag.BoolVar(&enableWatchListAlphaFeature, "enableWatchListFeature", false, "whether to set KUBE_FEATURE_WatchListClient env var")
}

func startInformersFor(ctx context.Context, client kubernetes.Interface, count int, namespace string) []cache.InformerSynced {
	var informersSynced []cache.InformerSynced
	for i := 0; i < count; i++ {
		inf := v1.NewSecretInformer(client, namespace, time.Duration(0), cache.Indexers{})
		informersSynced = append(informersSynced, inf.HasSynced)
		go inf.Run(ctx.Done())
	}
	return informersSynced
}
