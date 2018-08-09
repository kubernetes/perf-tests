/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/config"

	// ensure auth plugins are loaded
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	// AutomanagedNamespaceName is a basename for automanaged namespaces.
	AutomanagedNamespaceName = "namespace"
)

// Framework allows for interacting with Kubernetes cluster via
// official Kubernetes client.
type Framework struct {
	automanagedNamespaceCount int
	clientSet                 clientset.Interface
	dynamicClient             dynamic.Interface
}

// NewFramework creates new framework based on given kubeconfig.
func NewFramework(path string) (*Framework, error) {
	conf, err := config.PrepareConfig(path)
	if err != nil {
		return nil, fmt.Errorf("config prepare failed: %v", err)
	}
	clientSet, err := clientset.NewForConfig(conf)
	if err != nil {
		return nil, fmt.Errorf("creating clientset failed: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(conf)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic config failed: %v", err)
	}
	return &Framework{0, clientSet, dynamicClient}, nil
}

// GetClientSet returns clientSet client.
func (f *Framework) GetClientSet() clientset.Interface {
	return f.clientSet
}

// CreateAutomanagedNamespaces creates automanged namespaces.
func (f *Framework) CreateAutomanagedNamespaces(namespaceCount int) error {
	if f.automanagedNamespaceCount != 0 {
		return fmt.Errorf("automanaged namespaces already created")
	}
	for i := 0; i < namespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", AutomanagedNamespaceName, i)
		if err := client.CreateNamespace(f.clientSet, name); err != nil {
			return err
		}
		f.automanagedNamespaceCount++
	}
	return nil
}

// DeleteAutomanagedNamespaces deletes all automanged namespaces.
func (f *Framework) DeleteAutomanagedNamespaces() error {
	for i := 0; i < f.automanagedNamespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", AutomanagedNamespaceName, i)
		if err := client.DeleteNamespace(f.clientSet, name); err != nil {
			return err
		}
	}
	f.automanagedNamespaceCount = 0
	return nil
}

// CreateObject creates object base on given object description.
func (f *Framework) CreateObject(namespace string, name string, obj *unstructured.Unstructured) error {
	return client.CreateObject(f.dynamicClient, namespace, name, obj)
}

// PatchObject updates object (using patch) with given name using given object description.
func (f *Framework) PatchObject(namespace string, name string, obj *unstructured.Unstructured) error {
	return client.PatchObject(f.dynamicClient, namespace, name, obj)
}

// DeleteObject deletes object with given name and group-version-kind.
func (f *Framework) DeleteObject(gvk schema.GroupVersionKind, namespace string, name string) error {
	return client.DeleteObject(f.dynamicClient, gvk, namespace, name)
}

// GetObject retrieves object with given name and group-version-kind.
func (f *Framework) GetObject(gvk schema.GroupVersionKind, namespace string, name string) (*unstructured.Unstructured, error) {
	return client.GetObject(f.dynamicClient, gvk, namespace, name)
}
