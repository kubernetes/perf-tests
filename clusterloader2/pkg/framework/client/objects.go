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
	"fmt"
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
		apierrs.IsTooManyRequests(err) || utilnet.IsProbableEOF(err) || utilnet.IsConnectionReset(err) {
		return true
	}
	// If the error sends the Retry-After header, we respect it as an explicit confirmation we should retry.
	if _, shouldRetry := apierrs.SuggestsClientDelay(err); shouldRetry {
		return true
	}
	return false
}

func retryFunction(f func() error, isAllowedError func(error) bool) wait.ConditionFunc {
	return func() (bool, error) {
		err := f()
		if err == nil || (isAllowedError != nil && isAllowedError(err)) {
			return true, nil
		}
		if IsRetryableAPIError(err) {
			return false, nil
		}
		return false, err
	}
}

// CreateNamespace creates a single namespace with given name.
func CreateNamespace(c clientset.Interface, namespace string) error {
	createFunc := func() error {
		_, err := c.CoreV1().Namespaces().Create(&apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		return err
	}
	return RetryWithExponentialBackOff(retryFunction(createFunc, apierrs.IsAlreadyExists))

}

// DeleteNamespace deletes namespace with given name.
func DeleteNamespace(c clientset.Interface, namespace string) error {
	deleteFunc := func() error {
		return c.CoreV1().Namespaces().Delete(namespace, nil)
	}
	return RetryWithExponentialBackOff(retryFunction(deleteFunc, apierrs.IsNotFound))
}

// CreateObject creates object based on given object description.
func CreateObject(dynamicClient dynamic.Interface, namespace string, name string, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	obj.SetName(name)
	createFunc := func() error {
		_, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(obj, metav1.CreateOptions{})
		return err
	}
	return RetryWithExponentialBackOff(retryFunction(createFunc, apierrs.IsAlreadyExists))
}

// PatchObject updates (using patch) object with given name, group, version and kind based on given object description.
func PatchObject(dynamicClient dynamic.Interface, namespace string, name string, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	obj.SetName(name)
	updateFunc := func() error {
		currentObj, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		patch, err := createPatch(currentObj, obj)
		if err != nil {
			return fmt.Errorf("creating patch diff error: %v", err)
		}
		_, err = dynamicClient.Resource(gvr).Namespace(namespace).Patch(obj.GetName(), types.StrategicMergePatchType, patch, metav1.UpdateOptions{})
		return err
	}
	return RetryWithExponentialBackOff(retryFunction(updateFunc, nil))
}

// DeleteObject deletes object with given name, group, version and kind.
func DeleteObject(dynamicClient dynamic.Interface, gvk schema.GroupVersionKind, namespace string, name string) error {
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	deleteFunc := func() error {
		return dynamicClient.Resource(gvr).Namespace(namespace).Delete(name, nil)
	}
	return RetryWithExponentialBackOff(retryFunction(deleteFunc, apierrs.IsNotFound))
}

// GetObject retrieves object with given name, group, version and kind.
func GetObject(dynamicClient dynamic.Interface, gvk schema.GroupVersionKind, namespace string, name string) (*unstructured.Unstructured, error) {
	var obj *unstructured.Unstructured
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	getFunc := func() error {
		var err error
		// TODO(krzysied): Check in which cases IncludeUninitialized=true option is required -
		// implement additional handling if needed.
		obj, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(name, metav1.GetOptions{})
		return err
	}
	if err := RetryWithExponentialBackOff(retryFunction(getFunc, nil)); err != nil {
		return nil, err
	}
	return obj, nil
}

func createPatch(current, modified *unstructured.Unstructured) ([]byte, error) {
	currentJson, err := current.MarshalJSON()
	if err != nil {
		return []byte{}, err
	}
	modifiedJson, err := modified.MarshalJSON()
	if err != nil {
		return []byte{}, err
	}
	preconditions := []mergepatch.PreconditionFunc{mergepatch.RequireKeyUnchanged("apiVersion"),
		mergepatch.RequireKeyUnchanged("kind"), mergepatch.RequireMetadataKeyUnchanged("name")}
	// TODO(krzysied): Figure out way to pass original object or figure out way to use CreateTwoWayMergePatch.
	return jsonmergepatch.CreateThreeWayJSONMergePatch(nil, modifiedJson, currentJson, preconditions...)
}
