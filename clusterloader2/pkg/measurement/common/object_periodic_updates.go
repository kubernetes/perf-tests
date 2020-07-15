/*
Copyright 2020 The Kubernetes Authors.

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

package common

import (
	"fmt"
	"strconv"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	objectsPeriodicUpdatesName = "ObjectsPeriodicUpdates"
	defaultNamespace           = "default"
)

func init() {
	if err := measurement.Register(objectsPeriodicUpdatesName, createObjectsPeriodicUpdatesMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", objectsPeriodicUpdatesName, err)
	}
}

func createObjectsPeriodicUpdatesMeasurement() measurement.Measurement {
	return &objectsPeriodicUpdatesMeasurement{}
}

type objectsPeriodicUpdatesMeasurement struct {}

func createAndKeepUpdating(obj *unstructured.Unstructured, objName string, config *measurement.MeasurementConfig) {
	err := config.ClusterFramework.CreateObject(defaultNamespace, objName, obj)
	if err != nil {
		klog.Warningf("creating %s object problem: %w", objName, err)
		return
	}
	klog.Infof("created object %s successfully", objName)
	for {
		time.Sleep(20 * time.Second)
		obj.SetLabels(map[string]string{
			"time_label": strconv.FormatInt(time.Now().Unix(), 10),
		})
		if err := config.ClusterFramework.PatchObject(defaultNamespace, objName, obj); err != nil {
			klog.Warningf("patching %s object problem: %w", objName, err)
			continue
		}
		klog.Infof("patched object %s successfully", objName)
	}
}

func createPodAndKeepUpdating(obj *unstructured.Unstructured, objName string, config *measurement.MeasurementConfig) {
	err := config.ClusterFramework.CreateObject(defaultNamespace, objName, obj)
	if err != nil {
		klog.Warningf("creating %s object problem: %w", objName, err)
		return
	}
	klog.Infof("created object %s successfully", objName)
	for {
		time.Sleep(20 * time.Second)
		unstructuredPod, err := config.ClusterFramework.GetObject(schema.GroupVersionKind{Kind: "Pod", Version: "v1"}, defaultNamespace, objName)
		if err != nil {
			klog.Warningf("problem with pod: %w", err)
			continue
		}
		unstructuredPod.SetLabels(map[string]string{
			"time_label": strconv.FormatInt(time.Now().Unix(), 10),
		})
		if err = config.ClusterFramework.PatchObject(defaultNamespace, objName, unstructuredPod); err != nil {
			klog.Warningf("patching %s object problem: %w", objName, err)
			continue
		}
		klog.Infof("patched object %s successfully", objName)
	}
}

func (p *objectsPeriodicUpdatesMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	// Pod
	pod := &apiv1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Pod",
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name: "container-name",
					Image: "k8s.gcr.io/pause:3.1",
				},
			},
		},
	}
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		return nil, fmt.Errorf("periodically updated pod problem: %w", err)
	}
	podObj := unstructured.Unstructured{
		Object: unstructuredPod,
	}
	go createPodAndKeepUpdating(&podObj, "pod-updated-periodically", config)


	// Service
	service := &apiv1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Service",
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port: 80,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 9376,
					},
				},
			},
		},
	}
	unstructuredService, err := runtime.DefaultUnstructuredConverter.ToUnstructured(service)
	if err != nil {
		return nil, fmt.Errorf("periodically updated service problem: %w", err)
	}
	serviceObj := unstructured.Unstructured{
		Object: unstructuredService,
	}
	go createAndKeepUpdating(&serviceObj, "service-updated-periodically", config)


	// Secret
	secret := &apiv1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Secret",
		},
		Type: apiv1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte("dXNlcg=="),
			"password": []byte("cXdlcnR5"),
		},
	}
	unstructuredSecret, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		return nil, fmt.Errorf("periodically updated secret problem: %w", err)
	}
	secretObj := unstructured.Unstructured{
		Object: unstructuredSecret,
	}
	go createAndKeepUpdating(&secretObj, "secret-updated-periodically", config)


	// ConfigMap
	configMap := &apiv1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "ConfigMap",
		},
		Data: map[string]string{
			"key": "value",
		},
	}
	unstructuredConfigMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(configMap)
	if err != nil {
		return nil, fmt.Errorf("periodically updated ConfigMap problem: %w", err)
	}
	configMapObj := unstructured.Unstructured{
		Object: unstructuredConfigMap,
	}
	go createAndKeepUpdating(&configMapObj, "configmap-updated-periodically", config)


	// Endpoints
	endpoints := &apiv1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind: "Endpoints",
		},
		Subsets: []apiv1.EndpointSubset{},
	}
	unstructuredEndpoints, err := runtime.DefaultUnstructuredConverter.ToUnstructured(endpoints)
	if err != nil {
		return nil, fmt.Errorf("periodically updated Endpoints problem: %w", err)
	}
	endpointsObj := unstructured.Unstructured{
		Object: unstructuredEndpoints,
	}
	go createAndKeepUpdating(&endpointsObj, "endpoints-updated-periodically", config)

	return nil, nil
}

func (p *objectsPeriodicUpdatesMeasurement) Dispose() {}

func (p *objectsPeriodicUpdatesMeasurement) String() string {
	return objectsPeriodicUpdatesName
}
