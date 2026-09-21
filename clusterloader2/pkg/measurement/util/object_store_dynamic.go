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

/*
This file is copy of https://github.com/kubernetes/kubernetes/blob/master/test/utils/pod_store.go
with slight changes regarding labelSelector and flagSelector applied.
*/

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// ListObjectSimplifications returns a list of simplified objects using the provided lister.
func ListObjectSimplifications(lister cache.GenericLister, namespaces map[string]bool, labelSelector labels.Selector) ([]ObjectSimplification, error) {
	selector := labels.Everything()
	if labelSelector != nil {
		selector = labelSelector
	}
	objects, err := lister.List(selector)
	if err != nil {
		return nil, err
	}

	allNamespaces := len(namespaces) == 0
	result := make([]ObjectSimplification, 0, len(objects))
	for _, o := range objects {
		os, err := getObjectSimplification(o)
		if err != nil {
			return nil, err
		}
		if !allNamespaces && !namespaces[os.Metadata.Namespace] {
			continue
		}
		result = append(result, os)
	}
	return result, nil
}

// ObjectSimplification represents the content of the object
// that is needed to be handled by this measurement.
type ObjectSimplification struct {
	Metadata metav1.ObjectMeta    `json:"metadata"`
	Status   StatusWithConditions `json:"status"`
}

// StatusWithConditions represents the content of the status field
// that is required to be handled by this measurement.
type StatusWithConditions struct {
	Conditions []metav1.Condition `json:"conditions"`
}

func (o ObjectSimplification) String() string {
	return fmt.Sprintf("%s/%s", o.Metadata.Namespace, o.Metadata.Name)
}

func getObjectSimplification(o runtime.Object) (ObjectSimplification, error) {
	// Fast path for typed Pods
	if pod, ok := o.(*corev1.Pod); ok {
		var conditions []metav1.Condition
		for _, c := range pod.Status.Conditions {
			conditions = append(conditions, metav1.Condition{
				Type:   string(c.Type),
				Status: metav1.ConditionStatus(c.Status),
			})
		}
		return ObjectSimplification{
			Metadata: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
			Status: StatusWithConditions{
				Conditions: conditions,
			},
		}, nil
	}

	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		dataMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
		if err != nil {
			return ObjectSimplification{}, err
		}
		u = &unstructured.Unstructured{Object: dataMap}
	}

	conditionsSlice, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	var conditions []metav1.Condition
	for _, c := range conditionsSlice {
		cMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cType, _ := cMap["type"].(string)
		cStatus, _ := cMap["status"].(string)
		conditions = append(conditions, metav1.Condition{
			Type:   cType,
			Status: metav1.ConditionStatus(cStatus),
		})
	}
	return ObjectSimplification{
		Metadata: metav1.ObjectMeta{
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
		},
		Status: StatusWithConditions{
			Conditions: conditions,
		},
	}, nil
}
