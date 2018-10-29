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

package runtimeobjects

import (
	"fmt"
	"strconv"

	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
)

// GetObjectForKind returns object instance of given kind.
func GetObjectForKind(kind string) (runtime.Object, error) {
	switch kind {
	case "ReplicationController":
		return &corev1.ReplicationController{}, nil
	case "ReplicaSet":
		return &extensions.ReplicaSet{}, nil
	case "Deployment":
		return &extensions.Deployment{}, nil
	case "DaemonSet":
		return &extensions.DaemonSet{}, nil
	case "Job":
		return &batch.Job{}, nil
	default:
		return nil, fmt.Errorf("unsupported kind when getting runtime object: %v", kind)
	}
}

// ListRuntimeObjectsForKind returns objects of given kind that satisfy given namespace, labelSelector and fieldSelector.
func ListRuntimeObjectsForKind(c clientset.Interface, kind, namespace, labelSelector, fieldSelector string) ([]runtime.Object, error) {
	listOpts := metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
	}
	switch kind {
	case "ReplicationController":
		list, err := c.CoreV1().ReplicationControllers(namespace).List(listOpts)
		if err != nil {
			return nil, err
		}
		runtimeObjectsList := make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return runtimeObjectsList, nil
	case "ReplicaSet":
		list, err := c.ExtensionsV1beta1().ReplicaSets(namespace).List(listOpts)
		if err != nil {
			return nil, err
		}
		runtimeObjectsList := make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return runtimeObjectsList, nil
	case "Deployment":
		list, err := c.ExtensionsV1beta1().Deployments(namespace).List(listOpts)
		if err != nil {
			return nil, err
		}
		runtimeObjectsList := make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return runtimeObjectsList, nil
	case "DaemonSet":
		list, err := c.ExtensionsV1beta1().DaemonSets(namespace).List(listOpts)
		if err != nil {
			return nil, err
		}
		runtimeObjectsList := make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return runtimeObjectsList, nil
	case "Job":
		list, err := c.BatchV1().Jobs(namespace).List(listOpts)
		if err != nil {
			return nil, err
		}
		runtimeObjectsList := make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return runtimeObjectsList, nil
	default:
		return nil, fmt.Errorf("unsupported kind when getting runtime object: %v", kind)
	}
}

// GetNameFromRuntimeObject returns name of given runtime object.
func GetNameFromRuntimeObject(obj runtime.Object) (string, error) {
	metaObjectAccessor, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		return "", fmt.Errorf("unsupported kind when getting name: %v", obj)
	}
	return metaObjectAccessor.GetObjectMeta().GetName(), nil
}

// GetResourceVersionFromRuntimeObject returns resource version of given runtime object.
func GetResourceVersionFromRuntimeObject(obj runtime.Object) (uint64, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return 0, fmt.Errorf("accessor error: %v", err)
	}
	version := accessor.GetResourceVersion()
	if len(version) == 0 {
		return 0, nil
	}
	return strconv.ParseUint(version, 10, 64)
}

// GetNamespaceFromRuntimeObject returns namespace of given runtime object.
func GetNamespaceFromRuntimeObject(obj runtime.Object) (string, error) {
	metaObjectAccessor, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		return "", fmt.Errorf("unsupported kind when getting namespace: %v", obj)
	}
	return metaObjectAccessor.GetObjectMeta().GetNamespace(), nil
}

// GetSelectorFromRuntimeObject returns selector of given runtime object.
func GetSelectorFromRuntimeObject(obj runtime.Object) (labels.Selector, error) {
	switch typed := obj.(type) {
	case *corev1.ReplicationController:
		return labels.SelectorFromSet(typed.Spec.Selector), nil
	case *extensions.ReplicaSet:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *extensions.Deployment:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *extensions.DaemonSet:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *batch.Job:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	default:
		return nil, fmt.Errorf("unsupported kind when getting selector: %v", obj)
	}
}

// GetSpecFromRuntimeObject returns spec of given runtime object.
func GetSpecFromRuntimeObject(obj runtime.Object) (interface{}, error) {
	switch typed := obj.(type) {
	case *corev1.ReplicationController:
		return typed.Spec, nil
	case *extensions.ReplicaSet:
		return typed.Spec, nil
	case *extensions.Deployment:
		return typed.Spec, nil
	case *extensions.DaemonSet:
		return typed.Spec, nil
	case *batch.Job:
		return typed.Spec, nil
	default:
		return nil, fmt.Errorf("unsupported kind when getting selector: %v", obj)
	}
}

// GetReplicasFromRuntimeObject returns replicas number from given runtime object.
func GetReplicasFromRuntimeObject(obj runtime.Object) (int32, error) {
	switch typed := obj.(type) {
	case *corev1.ReplicationController:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *extensions.ReplicaSet:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *extensions.Deployment:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *extensions.DaemonSet:
		return 0, nil
	case *batch.Job:
		if typed.Spec.Parallelism != nil {
			return *typed.Spec.Parallelism, nil
		}
		return 0, nil
	default:
		return -1, fmt.Errorf("unsupported kind when getting number of replicas: %v", obj)
	}
}

// IsEqualRuntimeObjectsSpec returns true if given runtime objects have identical specs.
func IsEqualRuntimeObjectsSpec(runtimeObj1, runtimeObj2 runtime.Object) (bool, error) {
	runtimeObj1Spec, err := GetSpecFromRuntimeObject(runtimeObj1)
	if err != nil {
		return false, err
	}
	runtimeObj2Spec, err := GetSpecFromRuntimeObject(runtimeObj2)
	if err != nil {
		return false, err
	}
	return equality.Semantic.DeepEqual(runtimeObj1Spec, runtimeObj2Spec), nil
}
