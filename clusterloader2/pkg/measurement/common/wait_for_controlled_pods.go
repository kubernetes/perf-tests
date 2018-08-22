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

/*
TODO(krzysied): Replace ClientSet interface with dynamic client.
This will allow more generic approach.
*/

package common

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForRCPodsTimeout      = 60 * time.Second
	defaultWaitForRCPodsInterval     = 5 * time.Second
	waitForControlledPodsRunningName = "WaitForControlledPodsRunning"
)

func init() {
	measurement.Register(waitForControlledPodsRunningName, createWaitForControlledPodsRunningMeasurement)
}

func createWaitForControlledPodsRunningMeasurement() measurement.Measurement {
	return &waitForControlledPodsRunningMeasurement{}
}

type waitForControlledPodsRunningMeasurement struct{}

// Execute waits until all specified RCs have all pods running or until timeout happens.
// RCs can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
func (*waitForControlledPodsRunningMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	kind, err := util.GetString(config.Params, "kind")
	if err != nil {
		return summaries, err
	}
	namespace, err := util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
	if err != nil {
		return summaries, err
	}
	labelSelector, err := util.GetStringOrDefault(config.Params, "labelSelector", "")
	if err != nil {
		return summaries, err
	}
	fieldSelector, err := util.GetStringOrDefault(config.Params, "fieldSelector", "")
	if err != nil {
		return summaries, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForRCPodsTimeout)
	if err != nil {
		return summaries, err
	}

	runtimeObjectsList, err := getRuntimeObjectsForKind(config.ClientSet, kind, namespace, labelSelector, fieldSelector)
	if err != nil {
		return summaries, err
	}
	runningRuntimeObjects := 0
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(defaultWaitForRCPodsInterval) {
		if runningRuntimeObjects >= len(runtimeObjectsList) {
			return summaries, nil
		}

		if err := waitForRuntimeObject(config.ClientSet, runtimeObjectsList[runningRuntimeObjects], timeout-time.Since(start)); err != nil {
			return summaries, fmt.Errorf("waiting for %v error: %v", kind, err)
		}
		runningRuntimeObjects++
		if namespace == metav1.NamespaceAll {
			glog.Infof("%s: %d / %d %ss with all pods running", waitForControlledPodsRunningName, runningRuntimeObjects, len(runtimeObjectsList), kind)
		} else {
			glog.Infof("%s: %v: %d / %d %ss with all pods running", waitForControlledPodsRunningName, namespace, runningRuntimeObjects, len(runtimeObjectsList), kind)
		}
	}

	return summaries, fmt.Errorf("timeout while waiting for %ss to be running in namespace '%v' with labels '%v' and fields '%v' - only %d found running with all pods", kind, namespace, labelSelector, fieldSelector, runningRuntimeObjects)
}

func waitForRuntimeObject(c clientset.Interface, obj runtime.Object, timeout time.Duration) error {
	runtimeObjectNamespace, err := getNamespaceFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	runtimeObjectSelector, err := getSelectorFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	runtimeObjectReplicas, err := getReplicasFromRuntimeObject(obj)
	if err != nil {
		return err
	}

	err = waitForPods(c, runtimeObjectNamespace, runtimeObjectSelector.String(), "", int(runtimeObjectReplicas), timeout)
	if err != nil {
		return fmt.Errorf("waiting pods error: %v", err)
	}
	return nil
}

func getRuntimeObjectsForKind(c clientset.Interface, kind, namespace, labelSelector, fieldSelector string) ([]runtime.Object, error) {
	listOpts := metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
	}
	runtimeObjectsList := make([]runtime.Object, 0)
	switch kind {
	case "ReplicationController":
		list, err := c.CoreV1().ReplicationControllers(namespace).List(listOpts)
		if err != nil {
			return runtimeObjectsList, err
		}
		for i := range list.Items {
			runtimeObjectsList = append(runtimeObjectsList, &list.Items[i])
		}
		return runtimeObjectsList, nil
	case "ReplicaSet":
		list, err := c.ExtensionsV1beta1().ReplicaSets(namespace).List(listOpts)
		if err != nil {
			return runtimeObjectsList, err
		}
		for i := range list.Items {
			runtimeObjectsList = append(runtimeObjectsList, &list.Items[i])
		}
		return runtimeObjectsList, nil
	case "Deployment":
		list, err := c.ExtensionsV1beta1().Deployments(namespace).List(listOpts)
		if err != nil {
			return runtimeObjectsList, err
		}
		for i := range list.Items {
			runtimeObjectsList = append(runtimeObjectsList, &list.Items[i])
		}
		return runtimeObjectsList, nil
	case "DaemonSet":
		list, err := c.ExtensionsV1beta1().DaemonSets(namespace).List(listOpts)
		if err != nil {
			return runtimeObjectsList, err
		}
		for i := range list.Items {
			runtimeObjectsList = append(runtimeObjectsList, &list.Items[i])
		}
		return runtimeObjectsList, nil
	case "Job":
		list, err := c.BatchV1().Jobs(namespace).List(listOpts)
		if err != nil {
			return runtimeObjectsList, err
		}
		for i := range list.Items {
			runtimeObjectsList = append(runtimeObjectsList, &list.Items[i])
		}
		return runtimeObjectsList, nil
	default:
		return runtimeObjectsList, fmt.Errorf("unsupported kind when getting runtime object: %v", kind)
	}
}

func getNamespaceFromRuntimeObject(obj runtime.Object) (string, error) {
	metaObjectAccessor, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		return "", fmt.Errorf("unsupported kind when getting namespace: %v", obj)
	}
	return metaObjectAccessor.GetObjectMeta().GetNamespace(), nil
}

func getSelectorFromRuntimeObject(obj runtime.Object) (labels.Selector, error) {
	switch typed := obj.(type) {
	case *v1.ReplicationController:
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

func getReplicasFromRuntimeObject(obj runtime.Object) (int32, error) {
	switch typed := obj.(type) {
	case *v1.ReplicationController:
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
