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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	klog.InitFlags(fs)

	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig. Uses in-cluster config if empty.")
	namespace := fs.String("namespace", "", "Target namespace to watch (all namespaces if empty).")
	fieldSelector := fs.String("field-selector", "", "Optional field selector to filter watches (e.g. spec.nodeName=node-1).")
	labelSelector := fs.String("label-selector", "", "Optional label selector to filter watches.")
	apiVersion := fs.String("api-version", "", "apiVersion of the target resource.")
	resource := fs.String("resource", "", "resource name of the target resource.")
	contentyType := fs.String("content-type", "", "Content type for requests (required). Valid values: [json, proto]")
	watches := fs.Int("watches", 1, "Number of concurrent watches to maintain.")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *apiVersion != "v1" || (*resource != "pods" && *resource != "configmaps") {
		return fmt.Errorf("only v1/pods and v1/configmaps are supported for --api-version and --resource flags")
	}
	if *namespace == "" {
		return fmt.Errorf("--namespace must be non empty")
	}
	if *watches <= 0 {
		return fmt.Errorf("--watches must be > 0")
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		config, err = getConfig()
		if err != nil {
			return fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	}
	config.QPS = -1

	switch *contentyType {
	case "json":
		config.AcceptContentTypes = "application/json"
		config.ContentType = "application/json"
	case "proto":
		config.AcceptContentTypes = "application/vnd.kubernetes.protobuf"
		config.ContentType = "application/vnd.kubernetes.protobuf"
	default:
		return fmt.Errorf("only json,proto values are supported for --content-type")
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	ctx := context.Background()
	opts := []informers.SharedInformerOption{
		informers.WithNamespace(*namespace),
		// Transform objects to metadata-only so we don't hold full payloads in memory for every watcher.
		informers.WithTransform(func(obj any) (any, error) {
			if m, err := meta.Accessor(obj); err == nil {
				return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: m.GetName(), Namespace: m.GetNamespace()}}, nil
			}
			return obj, nil
		}),
		informers.WithTweakListOptions(func(listOpts *metav1.ListOptions) {
			listOpts.FieldSelector = *fieldSelector
			listOpts.LabelSelector = *labelSelector
		}),
	}

	klog.Infof("Starting pure watch workload: apiVersion=%q, resource=%q, namespace=%q, watches=%d, fieldSelector=%q", *apiVersion, *resource, *namespace, *watches, *fieldSelector)

	for range *watches {
		factory := informers.NewSharedInformerFactoryWithOptions(client, 0, opts...)
		switch *resource {
		case "pods":
			factory.Core().V1().Pods().Informer()
		case "configmaps":
			factory.Core().V1().ConfigMaps().Informer()
		}
		factory.Start(ctx.Done())
	}

	<-ctx.Done()
	return nil
}
