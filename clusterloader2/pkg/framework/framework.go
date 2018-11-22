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
	"regexp"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/config"

	// ensure auth plugins are loaded
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// Framework allows for interacting with Kubernetes cluster via
// official Kubernetes client.
type Framework struct {
	automanagedNamespacePrefix string
	automanagedNamespaceCount  int
	clientSet                  clientset.Interface
	dynamicClient              dynamic.Interface
}

// NewFramework creates new framework based on given kubeconfig.
func NewFramework(kubeconfigPath string) (*Framework, error) {
	conf, err := config.PrepareConfig(kubeconfigPath)
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
	return &Framework{
		automanagedNamespaceCount: 0,
		clientSet:                 clientSet,
		dynamicClient:             dynamicClient,
	}, nil
}

// GetAutomanagedNamespacePrefix returns automanaged namespace prefix.
func (f *Framework) GetAutomanagedNamespacePrefix() string {
	return f.automanagedNamespacePrefix
}

// SetAutomanagedNamespacePrefix sets automanaged namespace prefix.
func (f *Framework) SetAutomanagedNamespacePrefix(nsName string) {
	f.automanagedNamespacePrefix = nsName
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
	for i := 1; i <= namespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", f.automanagedNamespacePrefix, i)
		if err := client.CreateNamespace(f.clientSet, name); err != nil {
			return err
		}
		f.automanagedNamespaceCount++
	}
	return nil
}

// ListAutomanagedNamespaces returns all existing automanged namespace names.
func (f *Framework) ListAutomanagedNamespaces() ([]string, error) {
	var automanagedNamespacesList []string
	namespacesList, err := client.ListNamespaces(f.clientSet)
	if err != nil {
		return automanagedNamespacesList, err
	}
	for _, namespace := range namespacesList {
		matched, err := f.isAutomanagedNamespace(namespace.Name)
		if err != nil {
			return automanagedNamespacesList, err
		}
		if matched {
			automanagedNamespacesList = append(automanagedNamespacesList, namespace.Name)
		}
	}
	return automanagedNamespacesList, nil
}

// DeleteAutomanagedNamespaces deletes all automanged namespaces.
func (f *Framework) DeleteAutomanagedNamespaces() *errors.ErrorList {
	var wg wait.Group
	errList := errors.NewErrorList()
	for i := 1; i <= f.automanagedNamespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", f.automanagedNamespacePrefix, i)
		wg.Start(func() {
			if err := client.DeleteNamespace(f.clientSet, name); err != nil {
				errList.Append(err)
				return
			}
			if err := client.WaitForDeleteNamespace(f.clientSet, name); err != nil {
				errList.Append(err)
			}
		})
	}
	wg.Wait()
	f.automanagedNamespaceCount = 0
	return errList
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

func (f *Framework) isAutomanagedNamespace(name string) (bool, error) {
	return regexp.MatchString(f.automanagedNamespacePrefix+"-[1-9][0-9]*", name)
}
