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
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"

	// ensure auth plugins are loaded
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var namespaceID = regexp.MustCompile(`^test-[a-z0-9]+-[0-9]+$`)

const ParallelEventDeletionsName = "parallel-event-deletions"

// Framework allows for interacting with Kubernetes cluster via
// official Kubernetes client.
type Framework struct {
	automanagedNamespacePrefix string
	automanagedNamespaceCount  int
	clientSets                 *MultiClientSet
	dynamicClients             *MultiDynamicClient
	clusterConfig              *config.ClusterConfig
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
	var err error
	f := Framework{
		automanagedNamespaceCount: 0,
		clusterConfig:             clusterConfig,
	}
	if f.clientSets, err = NewMultiClientSet(kubeConfigPath, clientsNumber); err != nil {
		return nil, fmt.Errorf("multi client set creation error: %v", err)
	}
	if f.dynamicClients, err = NewMultiDynamicClient(kubeConfigPath, clientsNumber); err != nil {
		return nil, fmt.Errorf("multi dynamic client creation error: %v", err)
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

// GetClusterConfig returns cluster config.
func (f *Framework) GetClusterConfig() *config.ClusterConfig {
	return f.clusterConfig
}

// CreateAutomanagedNamespaces creates automanged namespaces.
func (f *Framework) CreateAutomanagedNamespaces(namespaceCount int) error {
	if f.automanagedNamespaceCount != 0 {
		return fmt.Errorf("automanaged namespaces already created")
	}

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
		}
		f.automanagedNamespaceCount++
	}
	return nil
}

// ListAutomanagedNamespaces returns all existing automanged namespace names.
func (f *Framework) ListAutomanagedNamespaces() ([]string, []string, error) {
	var automanagedNamespacesList, staleNamespaces []string
	namespacesList, err := client.ListNamespaces(f.clientSets.GetClient())
	if err != nil {
		return automanagedNamespacesList, staleNamespaces, err
	}
	for _, namespace := range namespacesList {
		matched, err := f.isAutomanagedNamespace(namespace.Name)
		if err != nil {
			return automanagedNamespacesList, staleNamespaces, err
		}
		if matched {
			automanagedNamespacesList = append(automanagedNamespacesList, namespace.Name)
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
	return automanagedNamespacesList, staleNamespaces, nil
}

func (f *Framework) deleteNamespace(namespace string) error {
	if f.clusterConfig.ParallelEventDeletions > 0 {
		f.tryFastDeleteEvents(namespace)
	}

	clientSet := f.clientSets.GetClient()
	if err := client.DeleteNamespace(clientSet, namespace); err != nil {
		return err
	}
	startWait := time.Now()
	if err := client.WaitForDeleteNamespace(clientSet, namespace); err != nil {
		return err
	}
	if time.Since(startWait) > time.Minute && f.clusterConfig.ParallelEventDeletions == 0 {
		klog.V(2).Infof("For faster cleanup of namespaces, consider setting %s on the command line", ParallelEventDeletionsName)
	}
	return nil
}

// tryDeleteEvents tries to delete events quickly. This is useful for faster
// cleanup in cases where there are large numbers of events - e.g. 100,000.
// Testing suggests that, in at least some cases, it's an order of magnitude faster
// to call this before deleting the namespace, versus just deleting the namespace.
// (e.g. 30 secs instead of 5 mins).
// Returns no error because, if it fails, deleting the namespace will delete the events
// anyway (but more slowly).
func (f *Framework) tryFastDeleteEvents(namespace string) {
	klog.V(2).Infof("Deleting events in parallel to speed up namespace deletion")
	start := time.Now()
	totalDeletions := int32(0)
	eventNames := make(chan string, 10000)
	wg := &sync.WaitGroup{}
	// Define one rate limiter for all deletions.
	// It allows deletions to proceed at a very high rate, so that in all but the most
	// extreme cases the rate will be governed only by f.ParallelEventDeletions and not
	// constrained by any rate limiting
	deleteRateLimiter := flowcontrol.NewTokenBucketRateLimiter(3000, 3000)

	// define worker
	deleteEventsWorker := func() {
		defer wg.Done()
		eventClient := f.clientSets.GetClient().EventsV1beta1().RESTClient()
		for {
			name, ok := <-eventNames
			if !ok {
				return
			}
			err := f.deleteWithRateLimiter(context.Background(),
				eventClient,
				namespace,
				"events",
				name,
				deleteRateLimiter)
			if err != nil {
				klog.V(2).Infof("Error deleting event: %v", err)
				return
			}
			atomic.AddInt32(&totalDeletions, 1)
		}
	}

	// feed event names into the channel
	go func() {
		continuation := ""
		eventClient := f.clientSets.GetClient().EventsV1beta1().Events(namespace)
		for {
			options := metav1.ListOptions{Limit: 1000, Continue: continuation}
			list, err := eventClient.List(context.Background(), options)
			if err != nil {
				close(eventNames)
				klog.V(2).Infof("Error listing events: %v", err)
				return
			}
			for _, e := range list.Items {
				eventNames <- e.Name
			}
			if list.Continue == "" {
				close(eventNames)
				return
			} else {
				continuation = list.Continue
			}
		}
	}()

	// delete in parallel
	concurrentOps := f.clusterConfig.ParallelEventDeletions
	for grNum := 0; grNum < concurrentOps; grNum++ {
		wg.Add(1)
		go deleteEventsWorker()
	}
	wg.Wait()

	klog.V(2).Infof("Deleted %d events in %v", atomic.LoadInt32(&totalDeletions), time.Since(start))
}

// deleteWithRateLimiter is just like func (c *events) Delete in event.go, except it allows
// overriding the rate limiter.
// An alternative would be to mess with the rate limiter in the underlying REST client, but
// that seems inconsistent with the patterns in the rest of ClusterLoader2, hence the creation of
// this method.
// BTW, without this method, we end up constrained by the 100 QPS limits of CL2s default rest client(s)
// and that means our event deletion isn't fast enough.  The whole point of parallel event deletion is
// to delete them quickly!
func (_ *Framework) deleteWithRateLimiter(ctx context.Context, client rest.Interface, namespace string, resourceType string, name string, r flowcontrol.RateLimiter) error {
	opts := metav1.DeleteOptions{}
	return client.Verb("DELETE").
		Namespace(namespace).
		Resource(resourceType).
		Name(name).
		Body(&opts).
		Throttle(r). // replace rate limiter on this request (not on underlying Rest Client)
		Do(ctx).
		Error()
}

// DeleteAutomanagedNamespaces deletes all automanged namespaces.
func (f *Framework) DeleteAutomanagedNamespaces() *errors.ErrorList {
	var wg wait.Group
	errList := errors.NewErrorList()
	for i := 1; i <= f.automanagedNamespaceCount; i++ {
		name := fmt.Sprintf("%v-%d", f.automanagedNamespacePrefix, i)
		wg.Start(func() {
			if err := f.deleteNamespace(name); err != nil {
				errList.Append(err)
				return
			}
		})
	}
	wg.Wait()
	f.automanagedNamespaceCount = 0
	return errList
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

func (f *Framework) isAutomanagedNamespace(name string) (bool, error) {
	return regexp.MatchString(f.automanagedNamespacePrefix+"-[1-9][0-9]*", name)
}

func (f *Framework) isStaleAutomanagedNamespace(name string) bool {
	return namespaceID.MatchString(name)
}
