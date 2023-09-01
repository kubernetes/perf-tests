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

package common

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForGenericK8sObjectsTimeout  = 30 * time.Minute
	defaultWaitForGenericK8sObjectsInterval = 30 * time.Second
	waitForGenericK8sObjectsMeasurementName = "WaitForGenericK8sObjects"
)

func init() {
	if err := measurement.Register(waitForGenericK8sObjectsMeasurementName, createWaitForGenericK8sObjectsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", waitForGenericK8sObjectsMeasurementName, err)
	}
}

func createWaitForGenericK8sObjectsMeasurement() measurement.Measurement {
	return &waitForGenericK8sObjectsMeasurement{}
}

type waitForGenericK8sObjectsMeasurement struct{}

// Execute waits until desired number of k8s objects reached given conditions.
// Conditions can denote either success or failure. The measurement assumes the
// k8s object has a status.conditions field, which contains a []metav1.Condition.
// More here: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
// Measurement will timeout if not enough objects have required conditions.
func (w *waitForGenericK8sObjectsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	groupVersionResource, err := getGroupVersionResource(config.Params)
	if err != nil {
		return nil, err
	}
	namespaces, err := getNamespaces(config.ClusterFramework.GetAutomanagedNamespacePrefix(), config.Params)
	if err != nil {
		return nil, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForGenericK8sObjectsTimeout)
	if err != nil {
		return nil, err
	}
	refreshInterval, err := util.GetDurationOrDefault(config.Params, "refreshInterval", defaultWaitForGenericK8sObjectsInterval)
	if err != nil {
		return nil, err
	}
	successfulConditions, err := util.GetStringArray(config.Params, "successfulConditions")
	if err != nil {
		return nil, err
	}
	failedConditions, err := util.GetStringArray(config.Params, "failedConditions")
	if err != nil {
		return nil, err
	}
	minDesiredObjectCount, err := util.GetInt(config.Params, "minDesiredObjectCount")
	if err != nil {
		return nil, err
	}
	maxFailedObjectCount, err := util.GetInt(config.Params, "maxFailedObjectCount")
	if err != nil {
		return nil, err
	}

	dynamicClient := config.ClusterFramework.GetDynamicClients().GetClient()
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	options := &measurementutil.WaitForGenericK8sObjectsOptions{
		GroupVersionResource:  groupVersionResource,
		Namespaces:            namespaces,
		SuccessfulConditions:  successfulConditions,
		FailedConditions:      failedConditions,
		MinDesiredObjectCount: minDesiredObjectCount,
		MaxFailedObjectCount:  maxFailedObjectCount,
		CallerName:            w.String(),
		WaitInterval:          refreshInterval,
	}
	return nil, measurementutil.WaitForGenericK8sObjects(ctx, dynamicClient, options)
}

// Dispose cleans up after the measurement.
func (*waitForGenericK8sObjectsMeasurement) Dispose() {}

// String returns a string representation of the measurement.
func (*waitForGenericK8sObjectsMeasurement) String() string {
	return waitForGenericK8sObjectsMeasurementName
}

func getGroupVersionResource(params map[string]interface{}) (schema.GroupVersionResource, error) {
	group, err := util.GetString(params, "objectGroup")
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	version, err := util.GetString(params, "objectVersion")
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	resource, err := util.GetString(params, "objectResource")
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}, nil
}

func getNamespaces(namespacesPrefix string, params map[string]interface{}) (measurementutil.NamespacesRange, error) {
	namespaceRange, err := util.GetMap(params, "namespaceRange")
	if err != nil {
		return measurementutil.NamespacesRange{}, err
	}
	min, err := util.GetInt(namespaceRange, "min")
	if err != nil {
		return measurementutil.NamespacesRange{}, err
	}
	max, err := util.GetInt(namespaceRange, "max")
	if err != nil {
		return measurementutil.NamespacesRange{}, err
	}

	return measurementutil.NamespacesRange{
		Prefix: namespacesPrefix,
		Min:    min,
		Max:    max,
	}, nil
}
