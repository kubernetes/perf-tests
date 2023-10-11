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
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

const (
	defaultResyncInterval = 10 * time.Second
)

// DynamicObjectStore is a convenient wrapper around cache.GenericLister.
type DynamicObjectStore struct {
	cache.GenericLister
	namespaces map[string]bool
}

// NewDynamicObjectStore creates DynamicObjectStore based on given object version resource and selector.
func NewDynamicObjectStore(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespaces map[string]bool) (*DynamicObjectStore, error) {
	informerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, defaultResyncInterval)
	lister := informerFactory.ForResource(gvr).Lister()
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	return &DynamicObjectStore{
		GenericLister: lister,
		namespaces:    namespaces,
	}, nil
}

// ListObjectSimplifications returns list of objects with conditions for each object that was returned by lister.
func (s *DynamicObjectStore) ListObjectSimplifications() ([]ObjectSimplification, error) {
	objects, err := s.GenericLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	result := make([]ObjectSimplification, 0, len(objects))
	for _, o := range objects {
		os, err := getObjectSimplification(o)
		if err != nil {
			return nil, err
		}
		if !s.namespaces[os.Metadata.Namespace] {
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
	dataMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return ObjectSimplification{}, err
	}

	jsonBytes, err := json.Marshal(dataMap)
	if err != nil {
		return ObjectSimplification{}, err
	}

	object := ObjectSimplification{}
	err = json.Unmarshal(jsonBytes, &object)
	return object, err
}
