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

func runInformer(args []string) error {
	fs := flag.NewFlagSet("informer", flag.ExitOnError)
	klog.InitFlags(fs)
	// Opt into the new klog behavior so that -stderrthreshold is honored even
	// when -logtostderr=true (the default).
	// Ref: kubernetes/klog#212, kubernetes/klog#432
	fs.Set("legacy_stderr_threshold_behavior", "false") //nolint:errcheck
	fs.Set("stderrthreshold", "INFO")                   //nolint:errcheck

	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig.")
	targetNamespace := fs.String("namespace", "", "namespace to run informers for. If empty will open on all namespaces.")
	informerCount := fs.Int("count", 4, "the number of informers per namespace to run.")
	testTimeout := fs.Duration("timeout", time.Minute, "timeout duration for the test")
	enableWatchListFeature := fs.Bool("enableWatchListFeature", false, "whether to set KUBE_FEATURE_WatchListClient env var")
	disableCompression := fs.Bool("disableCompression", false, "whether to disable gzip compression for API requests")
	apiVersion := fs.String("api-version", "v1", "apiVersion of the target resource (e.g. v1, apps/v1).")
	resource := fs.String("resource", "secrets", "resource name of the target resource (e.g. pods, deployments).")

	if err := fs.Parse(args); err != nil {
		return err
	}

	klog.Info("The informer subcommand started with the following arguments:")
	fs.VisitAll(func(f *flag.Flag) {
		klog.Infof("  -%s=%v (%s)\n", f.Name, f.Value, f.Usage)
	})

	if err := os.Setenv("KUBE_FEATURE_WatchListClient", strconv.FormatBool(*enableWatchListFeature)); err != nil {
		return fmt.Errorf("failed to set KUBE_FEATURE_WatchListClient: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *testTimeout)
	defer cancel()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build config: %w", err)
	}
	config.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	config.ContentType = "application/vnd.kubernetes.protobuf"
	config.DisableCompression = *disableCompression
	klog.Infof("The following Kubernetes client config will be used\n%v", config.String())

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	gv, err := schema.ParseGroupVersion(*apiVersion)
	if err != nil {
		return fmt.Errorf("failed to parse api-version %q: %w", *apiVersion, err)
	}
	targetGVR := gv.WithResource(*resource)

	err = wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		ts := time.Now()
		ctxInformer, cancelInformers := context.WithCancel(ctx)
		defer cancelInformers()

		klog.Infof("Starting %d informers for gvr = %v, targetNamespace = %q", *informerCount, targetGVR, *targetNamespace)
		informersSynced, err := startInformersForResource(ctxInformer, client, targetGVR, *informerCount, *targetNamespace)
		if err != nil {
			return false, err
		}

		klog.Infof("Waiting for gvr = %v informers to sync", targetGVR)
		if ok := cache.WaitForCacheSync(ctx.Done(), informersSynced...); !ok {
			return false, fmt.Errorf("timed out waiting for gvr %v informers to sync: %w", targetGVR, ctx.Err())
		}
		klog.Infof("All %v informers for gvr = %v synced, time needed = %v", len(informersSynced), targetGVR, time.Since(ts))
		return false, nil
	})
	if err != nil && !isInformerContextDoneErr(err) {
		return err
	}
	klog.Info("Exiting the informer subcommand")
	return nil
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

func isInformerContextDoneErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
