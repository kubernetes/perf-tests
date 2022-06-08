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
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	frconfig "k8s.io/perf-tests/clusterloader2/pkg/framework/config"

	restclient "k8s.io/client-go/rest"

	// ensure auth plugins are loaded
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var namespaceID = regexp.MustCompile(`^test-[a-z0-9]+-[0-9]+$`)

// Framework allows for interacting with Kubernetes cluster via
// official Kubernetes client.
type Framework struct {
	automanagedNamespacePrefix string
	mux                        sync.Mutex
	// automanagedNamespaces stores values as infromation if should the namespace be deleted
	automanagedNamespaces map[string]bool
	clientSets            *MultiClientSet
	dynamicClients        *MultiDynamicClient
	clusterConfig         *config.ClusterConfig
	restClientConfig      *restclient.Config
}

// NewFramework creates new framework based on given clusterConfig.
func NewFramework(clusterConfig *config.ClusterConfig, clientsNumber int) (*Framework, error) {
	return newFramework(clusterConfig, clientsNumber, clusterConfig.KubeConfigPath)
}

// NewRootFramework creates framework for the root cluster.
// For clusters other than kubemark there is no difference between NewRootFramework and NewFramework.
func NewRootFramework(clusterConfig *config.ClusterConfig, clientsNumber int) (*Framework, error) {
	kubeConfigPath := clusterConfig.KubeConfigPath
	if override := clusterConfig.Provider.GetConfig().RootFrameworkKubeConfigOverride(); override != "" {
		kubeConfigPath = override
	}
	return newFramework(clusterConfig, clientsNumber, kubeConfigPath)
}

func newFramework(clusterConfig *config.ClusterConfig, clientsNumber int, kubeConfigPath string) (*Framework, error) {
	klog.Infof("Creating framework with %d clients and %q kubeconfig.", clientsNumber, kubeConfigPath)
	var err error
	f := Framework{
		automanagedNamespaces: map[string]bool{},
		clusterConfig:         clusterConfig,
	}
	if f.clientSets, err = NewMultiClientSet(kubeConfigPath, clientsNumber); err != nil {
		return nil, fmt.Errorf("multi client set creation error: %v", err)
	}
	if f.dynamicClients, err = NewMultiDynamicClient(kubeConfigPath, clientsNumber); err != nil {
		return nil, fmt.Errorf("multi dynamic client creation error: %v", err)
	}

	if f.restClientConfig, err = frconfig.GetConfig(kubeConfigPath); err != nil {
		return nil, fmt.Errorf("rest client creation error: %v", err)
	}
	return &f, nil
}

// GetAutomanagedNamespacePrefix returns automanaged namespace prefix.
func (f *Framework) GetAutomanagedNamespacePrefix() string {
	return f.automanagedNamespacePrefix
}

// SetAutomanagedNamespacePrefix sets automanaged namespace prefix.
func (f *Framework) SetAutomanagedNamespacePrefix(nsName string) {
	f.automanagedNamespacePrefix = nsName
}

// GetClientSets returns clientSet clients.
func (f *Framework) GetClientSets() *MultiClientSet {
	return f.clientSets
}

// GetDynamicClients returns dynamic clients.
func (f *Framework) GetDynamicClients() *MultiDynamicClient {
	return f.dynamicClients
}

func (f *Framework) GetRestClient() *restclient.Config {
	return f.restClientConfig
}

// GetClusterConfig returns cluster config.
func (f *Framework) GetClusterConfig() *config.ClusterConfig {
	return f.clusterConfig
}

// CreateAutomanagedNamespaces creates automanged namespaces.
func (f *Framework) CreateAutomanagedNamespaces(namespaceCount int, allowExistingNamespaces bool, deleteAutomanagedNamespaces bool) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	// get all pre-created namespaces and store in a hash set
	namespacesList, err := client.ListNamespaces(f.clientSets.GetClient())
	if err != nil {
		return err
	}
	existingNamespaceSet := make(map[string]bool)
	for _, ns := range namespacesList {
		existingNamespaceSet[ns.Name] = true
	}

	for i := 1; i <= namespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", f.automanagedNamespacePrefix, i)
		if _, created := existingNamespaceSet[name]; !created {
			if err := client.CreateNamespace(f.clientSets.GetClient(), name); err != nil {
				return err
			}
		} else {
			if !allowExistingNamespaces {
				return fmt.Errorf("automanaged namespace %s already created", name)
			}
		}
		f.automanagedNamespaces[name] = deleteAutomanagedNamespaces
	}
	return nil
}

// ListAutomanagedNamespaces returns all existing automanged namespace names.
func (f *Framework) ListAutomanagedNamespaces() ([]string, []string, error) {
	var automanagedNamespacesCurrentPrefixList, staleNamespaces []string
	namespacesList, err := client.ListNamespaces(f.clientSets.GetClient())
	if err != nil {
		return automanagedNamespacesCurrentPrefixList, staleNamespaces, err
	}
	for _, namespace := range namespacesList {
		matched, err := f.isAutomanagedNamespaceCurrentPrefix(namespace.Name)
		if err != nil {
			return automanagedNamespacesCurrentPrefixList, staleNamespaces, err
		}
		if matched {
			automanagedNamespacesCurrentPrefixList = append(automanagedNamespacesCurrentPrefixList, namespace.Name)
		} else {
			// check further whether the namespace is a automanaged namespace created in previous test execution.
			// this could happen when the execution is aborted abornamlly, and the resource is not able to be
			// clean up.
			matched := f.isStaleAutomanagedNamespace(namespace.Name)
			if matched {
				staleNamespaces = append(staleNamespaces, namespace.Name)
			}
		}
	}
	return automanagedNamespacesCurrentPrefixList, staleNamespaces, nil
}

func (f *Framework) deleteNamespace(namespace string) error {
	clientSet := f.clientSets.GetClient()
	if err := client.DeleteNamespace(clientSet, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(clientSet, namespace); err != nil {
		return err
	}
	f.removeAutomanagedNamespace(namespace)
	return nil
}

// DeleteAutomanagedNamespaces deletes all automanged namespaces.
func (f *Framework) DeleteAutomanagedNamespaces() *errors.ErrorList {
	var wg wait.Group
	errList := errors.NewErrorList()
	for namespace, shouldBeDeleted := range f.getAutomanagedNamespaces() {
		namespace := namespace
		if shouldBeDeleted {
			wg.Start(func() {
				if err := f.deleteNamespace(namespace); err != nil {
					errList.Append(err)
					return
				}
			})
		}
	}
	wg.Wait()
	return errList
}

func (f *Framework) removeAutomanagedNamespace(namespace string) {
	f.mux.Lock()
	defer f.mux.Unlock()
	delete(f.automanagedNamespaces, namespace)
}

func (f *Framework) getAutomanagedNamespaces() map[string]bool {
	f.mux.Lock()
	defer f.mux.Unlock()
	m := map[string]bool{}
	for k, v := range f.automanagedNamespaces {
		m[k] = v
	}
	return m
}

// DeleteNamespaces deletes the list of namespaces.
func (f *Framework) DeleteNamespaces(namespaces []string) *errors.ErrorList {
	var wg wait.Group
	errList := errors.NewErrorList()
	for _, namespace := range namespaces {
		namespace := namespace
		wg.Start(func() {
			if err := f.deleteNamespace(namespace); err != nil {
				errList.Append(err)
				return
			}
		})
	}
	wg.Wait()
	return errList
}

// CreateObject creates object base on given object description.
func (f *Framework) CreateObject(namespace string, name string, obj *unstructured.Unstructured, options ...*client.APICallOptions) error {
	return client.CreateObject(f.dynamicClients.GetClient(), namespace, name, obj, options...)
}

// PatchObject updates object (using patch) with given name using given object description.
func (f *Framework) PatchObject(namespace string, name string, obj *unstructured.Unstructured, options ...*client.APICallOptions) error {
	return client.PatchObject(f.dynamicClients.GetClient(), namespace, name, obj)
}

// DeleteObject deletes object with given name and group-version-kind.
func (f *Framework) DeleteObject(gvk schema.GroupVersionKind, namespace string, name string, options ...*client.APICallOptions) error {
	return client.DeleteObject(f.dynamicClients.GetClient(), gvk, namespace, name)
}

// GetObject retrieves object with given name and group-version-kind.
func (f *Framework) GetObject(gvk schema.GroupVersionKind, namespace string, name string, options ...*client.APICallOptions) (*unstructured.Unstructured, error) {
	return client.GetObject(f.dynamicClients.GetClient(), gvk, namespace, name)
}

// ApplyTemplatedManifests finds and applies all manifest template files matching the provided
// manifestGlob pattern. It substitutes the template placeholders using the templateMapping map.
func (f *Framework) ApplyTemplatedManifests(manifestGlob string, templateMapping map[string]interface{}, options ...*client.APICallOptions) error {
	// TODO(mm4tt): Consider using the out-of-the-box "kubectl create -f".
	manifestGlob = os.ExpandEnv(manifestGlob)
	templateProvider := config.NewTemplateProvider(filepath.Dir(manifestGlob))
	manifests, err := filepath.Glob(manifestGlob)
	if err != nil {
		return err
	}
	if manifests == nil {
		klog.Warningf("There is no matching file for pattern %v.\n", manifestGlob)
	}
	for _, manifest := range manifests {
		klog.V(1).Infof("Applying %s\n", manifest)
		obj, err := templateProvider.TemplateToObject(filepath.Base(manifest), templateMapping)
		if err != nil {
			if err == config.ErrorEmptyFile {
				klog.Warningf("Skipping empty manifest %s", manifest)
				continue
			}
			return err
		}
		objList := []unstructured.Unstructured{*obj}
		if obj.IsList() {
			list, err := obj.ToList()
			if err != nil {
				return err
			}
			objList = list.Items
		}
		for _, item := range objList {
			if err := f.CreateObject(item.GetNamespace(), item.GetName(), &item, options...); err != nil {
				return fmt.Errorf("error while applying (%s): %v", manifest, err)
			}
		}

	}
	return nil
}

func (f *Framework) isAutomanagedNamespaceCurrentPrefix(name string) (bool, error) {
	return regexp.MatchString(f.automanagedNamespacePrefix+"-[1-9][0-9]*", name)
}

func (f *Framework) isStaleAutomanagedNamespace(name string) bool {
	if namespaceID.MatchString(name) {
		_, isFromThisExecution := f.getAutomanagedNamespaces()[name]
		return !isFromThisExecution
	}
	return false
}
