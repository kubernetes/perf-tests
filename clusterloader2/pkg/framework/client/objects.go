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

package client

import (
	"context"
	"fmt"
	"net"
	"time"

	apiv1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	// Parameters for retrying with exponential backoff.
	retryBackoffInitialDuration = 100 * time.Millisecond
	retryBackoffFactor          = 3
	retryBackoffJitter          = 0
	retryBackoffSteps           = 6

	// Parameters for namespace deletion operations.
	defaultNamespaceDeletionTimeout  = 10 * time.Minute
	defaultNamespaceDeletionInterval = 5 * time.Second

	// String const defined in https://go.googlesource.com/net/+/749bd193bc2bcebc5f1a048da8af0392cfb2fa5d/http2/transport.go#1041
	// TODO(mborsz): Migrate to error object comparison when the error type is exported.
	http2ClientConnectionLostErr = "http2: client connection lost"
)

// RetryWithExponentialBackOff a utility for retrying the given function with exponential backoff.
func RetryWithExponentialBackOff(fn wait.ConditionFunc) error {
	backoff := wait.Backoff{
		Duration: retryBackoffInitialDuration,
		Factor:   retryBackoffFactor,
		Jitter:   retryBackoffJitter,
		Steps:    retryBackoffSteps,
	}
	return wait.ExponentialBackoff(backoff, fn)
}

// IsRetryableAPIError verifies whether the error is retryable.
func IsRetryableAPIError(err error) bool {
	// These errors may indicate a transient error that we can retry in tests.
	if apierrs.IsInternalError(err) || apierrs.IsTimeout(err) || apierrs.IsServerTimeout(err) ||
		apierrs.IsTooManyRequests(err) || utilnet.IsProbableEOF(err) || utilnet.IsConnectionReset(err) ||
		// Retryable resource-quotas conflict errors may be returned in some cases, e.g. https://github.com/kubernetes/kubernetes/issues/67761
		isResourceQuotaConflictError(err) ||
		// Our client is using OAuth2 where 401 (unauthorized) can mean that our token has expired and we need to retry with a new one.
		apierrs.IsUnauthorized(err) {
		return true
	}
	// If the error sends the Retry-After header, we respect it as an explicit confirmation we should retry.
	if _, shouldRetry := apierrs.SuggestsClientDelay(err); shouldRetry {
		return true
	}
	return false
}

func isResourceQuotaConflictError(err error) bool {
	apiErr, ok := err.(apierrs.APIStatus)
	if !ok {
		return false
	}
	if apiErr.Status().Reason != metav1.StatusReasonConflict {
		return false
	}
	return apiErr.Status().Details != nil && apiErr.Status().Details.Kind == "resourcequotas"
}

// IsRetryableNetError determines whether the error is a retryable net error.
func IsRetryableNetError(err error) bool {
	if netError, ok := err.(net.Error); ok {
		return netError.Temporary() || netError.Timeout()
	}

	if err.Error() == http2ClientConnectionLostErr {
		return true
	}
	return false
}

// APICallOptions describes how api call errors should be treated, i.e. which errors should be
// allowed (ignored) and which should be retried.
type APICallOptions struct {
	shouldAllowError func(error) bool
	shouldRetryError func(error) bool
}

// Allow creates an APICallOptions that allows (ignores) errors matching the given predicate.
func Allow(allowErrorPredicate func(error) bool) *APICallOptions {
	return &APICallOptions{shouldAllowError: allowErrorPredicate}
}

// Retry creates an APICallOptions that retries errors matching the given predicate.
func Retry(retryErrorPredicate func(error) bool) *APICallOptions {
	return &APICallOptions{shouldRetryError: retryErrorPredicate}
}

// RetryFunction opaques given function into retryable function.
func RetryFunction(f func() error, options ...*APICallOptions) wait.ConditionFunc {
	var shouldAllowErrorFuncs, shouldRetryErrorFuncs []func(error) bool
	for _, option := range options {
		if option.shouldAllowError != nil {
			shouldAllowErrorFuncs = append(shouldAllowErrorFuncs, option.shouldAllowError)
		}
		if option.shouldRetryError != nil {
			shouldRetryErrorFuncs = append(shouldRetryErrorFuncs, option.shouldRetryError)
		}
	}
	return func() (bool, error) {
		err := f()
		if err == nil {
			return true, nil
		}
		if IsRetryableAPIError(err) || IsRetryableNetError(err) {
			return false, nil
		}
		for _, shouldAllowError := range shouldAllowErrorFuncs {
			if shouldAllowError(err) {
				return true, nil
			}
		}
		for _, shouldRetryError := range shouldRetryErrorFuncs {
			if shouldRetryError(err) {
				return false, nil
			}
		}
		return false, err
	}
}

// ListPodsWithOptions lists the pods using the provided options.
func ListPodsWithOptions(c clientset.Interface, namespace string, listOpts metav1.ListOptions) ([]apiv1.Pod, error) {
	var pods []apiv1.Pod
	listFunc := func() error {
		podsList, err := c.CoreV1().Pods(namespace).List(context.TODO(), listOpts)
		if err != nil {
			return err
		}
		pods = podsList.Items
		return nil
	}
	if err := RetryWithExponentialBackOff(RetryFunction(listFunc)); err != nil {
		return pods, err
	}
	return pods, nil
}

// ListNodes returns list of cluster nodes.
func ListNodes(c clientset.Interface) ([]apiv1.Node, error) {
	return ListNodesWithOptions(c, metav1.ListOptions{})
}

// ListNodesWithOptions lists the cluster nodes using the provided options.
func ListNodesWithOptions(c clientset.Interface, listOpts metav1.ListOptions) ([]apiv1.Node, error) {
	var nodes []apiv1.Node
	listFunc := func() error {
		nodesList, err := c.CoreV1().Nodes().List(context.TODO(), listOpts)
		if err != nil {
			return err
		}
		nodes = nodesList.Items
		return nil
	}
	if err := RetryWithExponentialBackOff(RetryFunction(listFunc)); err != nil {
		return nodes, err
	}
	return nodes, nil
}

// CreateNamespace creates a single namespace with given name.
func CreateNamespace(c clientset.Interface, namespace string) error {
	createFunc := func() error {
		_, err := c.CoreV1().Namespaces().Create(context.TODO(), &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
		return err
	}
	return RetryWithExponentialBackOff(RetryFunction(createFunc, Allow(apierrs.IsAlreadyExists)))
}

// DeleteNamespace deletes namespace with given name.
func DeleteNamespace(c clientset.Interface, namespace string) error {
	deleteFunc := func() error {
		return c.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
	}
	return RetryWithExponentialBackOff(RetryFunction(deleteFunc, Allow(apierrs.IsNotFound)))
}

// ListNamespaces returns list of existing namespace names.
func ListNamespaces(c clientset.Interface) ([]apiv1.Namespace, error) {
	var namespaces []apiv1.Namespace
	listFunc := func() error {
		namespacesList, err := c.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		namespaces = namespacesList.Items
		return nil
	}
	if err := RetryWithExponentialBackOff(RetryFunction(listFunc)); err != nil {
		return namespaces, err
	}
	return namespaces, nil
}

// WaitForDeleteNamespace waits untils namespace is terminated.
func WaitForDeleteNamespace(c clientset.Interface, namespace string) error {
	retryWaitFunc := func() (bool, error) {
		_, err := c.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				return true, nil
			}
			if !IsRetryableAPIError(err) {
				return false, err
			}
		}
		return false, nil
	}
	return wait.PollImmediate(defaultNamespaceDeletionInterval, defaultNamespaceDeletionTimeout, retryWaitFunc)
}

// ListEvents retrieves events for the object with the given name.
func ListEvents(c clientset.Interface, namespace string, name string, options ...*APICallOptions) (obj *apiv1.EventList, err error) {
	getFunc := func() error {
		obj, err = c.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
			FieldSelector: "involvedObject.name=" + name,
		})
		return err
	}
	if err := RetryWithExponentialBackOff(RetryFunction(getFunc, options...)); err != nil {
		return nil, err
	}
	return obj, nil
}

// DeleteStorageClass deletes storage class with given name.
func DeleteStorageClass(c clientset.Interface, name string) error {
	deleteFunc := func() error {
		return c.StorageV1().StorageClasses().Delete(context.TODO(), name, metav1.DeleteOptions{})
	}
	return RetryWithExponentialBackOff(RetryFunction(deleteFunc, Allow(apierrs.IsNotFound)))
}

// CreateObject creates object based on given object description.
func CreateObject(dynamicClient dynamic.Interface, namespace string, name string, obj *unstructured.Unstructured, options ...*APICallOptions) error {
	gvk := obj.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	obj.SetName(name)
	createFunc := func() error {
		_, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
		return err
	}
	options = append(options, Allow(apierrs.IsAlreadyExists))
	return RetryWithExponentialBackOff(RetryFunction(createFunc, options...))
}

// PatchObject updates (using patch) object with given name, group, version and kind based on given object description.
func PatchObject(dynamicClient dynamic.Interface, namespace string, name string, obj *unstructured.Unstructured, options ...*APICallOptions) error {
	gvk := obj.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	obj.SetName(name)
	updateFunc := func() error {
		currentObj, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		patch, err := createPatch(currentObj, obj)
		if err != nil {
			return fmt.Errorf("creating patch diff error: %v", err)
		}
		_, err = dynamicClient.Resource(gvr).Namespace(namespace).Patch(context.TODO(), obj.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	}
	return RetryWithExponentialBackOff(RetryFunction(updateFunc, options...))
}

// DeleteObject deletes object with given name, group, version and kind.
func DeleteObject(dynamicClient dynamic.Interface, gvk schema.GroupVersionKind, namespace string, name string, options ...*APICallOptions) error {
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	deleteFunc := func() error {
		// Delete operation removes object with all of the dependants.
		policy := metav1.DeletePropagationBackground
		deleteOption := metav1.DeleteOptions{PropagationPolicy: &policy}
		return dynamicClient.Resource(gvr).Namespace(namespace).Delete(context.TODO(), name, deleteOption)
	}
	options = append(options, Allow(apierrs.IsNotFound))
	return RetryWithExponentialBackOff(RetryFunction(deleteFunc, options...))
}

// GetObject retrieves object with given name, group, version and kind.
func GetObject(dynamicClient dynamic.Interface, gvk schema.GroupVersionKind, namespace string, name string, options ...*APICallOptions) (*unstructured.Unstructured, error) {
	var obj *unstructured.Unstructured
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	getFunc := func() error {
		var err error
		// TODO(krzysied): Check in which cases IncludeUninitialized=true option is required -
		// implement additional handling if needed.
		obj, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		return err
	}
	if err := RetryWithExponentialBackOff(RetryFunction(getFunc, options...)); err != nil {
		return nil, err
	}
	return obj, nil
}

func createPatch(current, modified *unstructured.Unstructured) ([]byte, error) {
	currentJSON, err := current.MarshalJSON()
	if err != nil {
		return []byte{}, err
	}
	modifiedJSON, err := modified.MarshalJSON()
	if err != nil {
		return []byte{}, err
	}
	preconditions := []mergepatch.PreconditionFunc{mergepatch.RequireKeyUnchanged("apiVersion"),
		mergepatch.RequireKeyUnchanged("kind"), mergepatch.RequireMetadataKeyUnchanged("name")}
	// We are passing nil as original object to CreateThreeWayJSONMergePatch which has a drawback that
	// if some field has been deleted between `original` and `modified` object
	// (e.g. by removing field in object's yaml), we will never remove that field from 'current'.
	// TODO(mborsz): Pass here the original object.
	return jsonmergepatch.CreateThreeWayJSONMergePatch(nil /* original */, modifiedJSON, currentJSON, preconditions...)
}
