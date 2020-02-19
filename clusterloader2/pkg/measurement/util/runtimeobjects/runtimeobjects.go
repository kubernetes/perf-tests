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

	goerrors "github.com/go-errors/errors"
	appsv1 "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	v1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
)

// ListRuntimeObjectsForKind returns objects of given kind that satisfy given namespace, labelSelector and fieldSelector.
func ListRuntimeObjectsForKind(d dynamic.Interface, gvr schema.GroupVersionResource, kind, namespace, labelSelector, fieldSelector string) ([]runtime.Object, error) {
	var runtimeObjectsList []runtime.Object
	var listFunc func() error
	listOpts := metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
	}
	listFunc = func() error {
		list, err := d.Resource(gvr).List(listOpts)
		if err != nil {
			return err
		}
		runtimeObjectsList = make([]runtime.Object, len(list.Items))
		for i := range list.Items {
			runtimeObjectsList[i] = &list.Items[i]
		}
		return nil
	}

	if err := client.RetryWithExponentialBackOff(client.RetryFunction(listFunc)); err != nil {
		return nil, err
	}
	return runtimeObjectsList, nil
}

// GetNameFromRuntimeObject returns name of given runtime object.
func GetNameFromRuntimeObject(obj runtime.Object) (string, error) {
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return typed.GetName(), nil
	default:
		metaObjectAccessor, ok := obj.(metav1.ObjectMetaAccessor)
		if !ok {
			return "", fmt.Errorf("unsupported kind when getting name: %v", obj)
		}
		return metaObjectAccessor.GetObjectMeta().GetName(), nil
	}
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
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return typed.GetNamespace(), nil
	default:
		metaObjectAccessor, ok := obj.(metav1.ObjectMetaAccessor)
		if !ok {
			return "", fmt.Errorf("unsupported kind when getting namespace: %v", obj)
		}
		return metaObjectAccessor.GetObjectMeta().GetNamespace(), nil
	}
}

// GetSelectorFromRuntimeObject returns selector of given runtime object.
func GetSelectorFromRuntimeObject(obj runtime.Object) (labels.Selector, error) {
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getSelectorFromUnstrutured(typed)
	case *corev1.ReplicationController:
		return labels.SelectorFromSet(typed.Spec.Selector), nil
	case *appsv1.ReplicaSet:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *appsv1.Deployment:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *appsv1.StatefulSet:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *appsv1.DaemonSet:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *batch.Job:
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	default:
		return nil, fmt.Errorf("unsupported kind when getting selector: %v", obj)
	}
}

// Note: This function assumes each controller has field Spec.Selector.
// Moreover, Spec.Selector should be *metav1.LabelSelector, except for RelicationController.
func getSelectorFromUnstrutured(obj *unstructured.Unstructured) (labels.Selector, error) {
	spec, err := getSpecFromUnstrutured(obj)
	if err != nil {
		return nil, err
	}
	switch obj.GetKind() {
	case "ReplicationController":
		selectorMap, found, err := unstructured.NestedStringMap(spec, "selector")
		if err != nil {
			return nil, fmt.Errorf("try to selector failed, %v", err)
		}
		if !found {
			return nil, fmt.Errorf("try to selector failed, field selector not found")
		}
		return labels.SelectorFromSet(selectorMap), nil
	default:
		selectorMap, found, err := unstructured.NestedMap(spec, "selector")
		if err != nil {
			return nil, fmt.Errorf("try to selector failed, %v", err)
		}
		if !found {
			return nil, fmt.Errorf("try to selector failed, field selector not found")
		}
		var selector metav1.LabelSelector
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(selectorMap, &selector)
		if err != nil {
			return nil, fmt.Errorf("try to selector failed, %v", err)
		}
		return metav1.LabelSelectorAsSelector(&selector)
	}
}

// GetIsPodUpdatedPredicateFromRuntimeObject returns a func(*corev1.Pod) bool predicate
// that can be used to check if given pod represents the desired state of pod.
func GetIsPodUpdatedPredicateFromRuntimeObject(obj runtime.Object) (func(*corev1.Pod) bool, error) {
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getIsPodUpdatedPodPredicateFromUnstructured(typed)
	default:
		return nil, goerrors.Errorf("unsupported kind when getting updated pod predicate: %v", obj)
	}
}

func getIsPodUpdatedPodPredicateFromUnstructured(obj *unstructured.Unstructured) (func(_ *corev1.Pod) bool, error) {
	templateMap, ok, err := unstructured.NestedMap(obj.UnstructuredContent(), "spec", "template")
	if err != nil {
		return nil, goerrors.Errorf("failed to get pod template: %v", err)
	}
	if !ok {
		return nil, goerrors.Errorf("spec.template is not set in object %v", obj.UnstructuredContent())
	}
	template := corev1.PodTemplateSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(templateMap, &template); err != nil {
		return nil, goerrors.Errorf("failed to parse spec.teemplate as v1.PodTemplateSpec")
	}

	return func(pod *corev1.Pod) bool {
		return equality.Semantic.DeepDerivative(template.Spec, pod.Spec)
	}, nil
}

// GetSpecFromRuntimeObject returns spec of given runtime object.
func GetSpecFromRuntimeObject(obj runtime.Object) (interface{}, error) {
	if obj == nil {
		return nil, nil
	}
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getSpecFromUnstrutured(typed)
	case *corev1.ReplicationController:
		return typed.Spec, nil
	case *appsv1.ReplicaSet:
		return typed.Spec, nil
	case *appsv1.Deployment:
		return typed.Spec, nil
	case *appsv1.StatefulSet:
		return typed.Spec, nil
	case *appsv1.DaemonSet:
		return typed.Spec, nil
	case *batch.Job:
		return typed.Spec, nil
	default:
		return nil, fmt.Errorf("unsupported kind when getting spec: %v", obj)
	}
}

// Note: This function assumes each controller has field Spec.
func getSpecFromUnstrutured(obj *unstructured.Unstructured) (map[string]interface{}, error) {
	spec, ok, err := unstructured.NestedMap(obj.UnstructuredContent(), "spec")
	if err != nil {
		return nil, fmt.Errorf("try to acquire spec failed, %v", err)
	}
	if !ok {
		return nil, fmt.Errorf("try to acquire spec failed, no field spec for obj %s", obj.GetName())
	}
	return spec, nil
}

// GetReplicasFromRuntimeObject returns replicas number from given runtime object.
func GetReplicasFromRuntimeObject(c clientset.Interface, obj runtime.Object) (int32, error) {
	if obj == nil {
		return 0, nil
	}
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getReplicasFromUnstrutured(c, typed)
	case *corev1.ReplicationController:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *appsv1.ReplicaSet:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *appsv1.Deployment:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *appsv1.StatefulSet:
		if typed.Spec.Replicas != nil {
			return *typed.Spec.Replicas, nil
		}
		return 0, nil
	case *appsv1.DaemonSet:
		return getNumSchedulableNodesMatchingNodeSelectorAndNodeAffinity(c, typed.Spec.Template.Spec.NodeSelector, typed.Spec.Template.Spec.Affinity)
	case *batch.Job:
		if typed.Spec.Parallelism != nil {
			return *typed.Spec.Parallelism, nil
		}
		return 0, nil
	default:
		return -1, fmt.Errorf("unsupported kind when getting number of replicas: %v", obj)
	}
}

// getNumSchedulableNodesMatchingNodeSelectorAndNodeAffinity returns the number of schedulable nodes matching both nodeSelector and NodeAffinity.
func getNumSchedulableNodesMatchingNodeSelectorAndNodeAffinity(c clientset.Interface, nodeSelector map[string]string, affinity *corev1.Affinity) (int32, error) {
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(nodeSelector))
	if err != nil {
		return 0, err
	}
	listOpts := metav1.ListOptions{LabelSelector: selector.String()}
	nodeList, err := client.ListNodesWithOptions(c, listOpts)
	if err != nil {
		return 0, err
	}
	var numSchedulableNodes int32
	for i := range nodeList {
		matched, err := podMatchesNodeAffinity(affinity, &nodeList[i])
		if err != nil {
			return 0, err
		}
		if !nodeList[i].Spec.Unschedulable && matched {
			numSchedulableNodes++
		}
	}
	return numSchedulableNodes, nil
}

// Note: This function assumes each controller has field Spec.Replicas, except DaemonSets and Job.
func getReplicasFromUnstrutured(c clientset.Interface, obj *unstructured.Unstructured) (int32, error) {
	spec, err := getSpecFromUnstrutured(obj)
	if err != nil {
		return -1, err
	}
	return tryAcquireReplicasFromUnstructuredSpec(c, spec, obj.GetKind())
}

func tryAcquireReplicasFromUnstructuredSpec(c clientset.Interface, spec map[string]interface{}, kind string) (int32, error) {
	switch kind {
	case "DaemonSet":
		nodeSelector, err := getDaemonSetNodeSelectorFromUnstructuredSpec(spec)
		if err != nil {
			return 0, err
		}
		affinity, err := getDaemonSetAffinityFromUnstructuredSpec(spec)
		if err != nil {
			return 0, err
		}
		return getNumSchedulableNodesMatchingNodeSelectorAndNodeAffinity(c, nodeSelector, affinity)
	case "Job":
		replicas, found, err := unstructured.NestedInt64(spec, "parallelism")
		if err != nil {
			return -1, fmt.Errorf("try to acquire job parallelism failed, %v", err)
		}
		if !found {
			return 0, nil
		}
		return int32(replicas), nil
	default:
		replicas, found, err := unstructured.NestedInt64(spec, "replicas")
		if err != nil {
			return -1, fmt.Errorf("try to acquire replicas failed, %v", err)
		}
		if !found {
			return 0, nil
		}
		return int32(replicas), nil
	}
}

func getDaemonSetNodeSelectorFromUnstructuredSpec(spec map[string]interface{}) (map[string]string, error) {
	template, found, err := unstructured.NestedMap(spec, "template")
	if err != nil || !found {
		return nil, err
	}
	podSpec, found, err := unstructured.NestedMap(template, "spec")
	if err != nil || !found {
		return nil, err
	}
	nodeSelector, _, err := unstructured.NestedStringMap(podSpec, "nodeSelector")
	return nodeSelector, err
}

func getDaemonSetAffinityFromUnstructuredSpec(spec map[string]interface{}) (*corev1.Affinity, error) {
	template, found, err := unstructured.NestedMap(spec, "template")
	if err != nil || !found {
		return nil, err
	}
	podSpec, found, err := unstructured.NestedMap(template, "spec")
	if err != nil || !found {
		return nil, err
	}
	unstructuredAffinity, found, err := unstructured.NestedMap(podSpec, "affinity")
	if err != nil || !found {
		return nil, err
	}
	affinity := &corev1.Affinity{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredAffinity, affinity)
	return affinity, err
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

// CreateMetaNamespaceKey returns meta key (namespace/name) for given runtime object.
func CreateMetaNamespaceKey(obj runtime.Object) (string, error) {
	namespace, err := GetNamespaceFromRuntimeObject(obj)
	if err != nil {
		return "", fmt.Errorf("retrieving namespace error: %v", err)
	}
	name, err := GetNameFromRuntimeObject(obj)
	if err != nil {
		return "", fmt.Errorf("retrieving name error: %v", err)
	}
	return namespace + "/" + name, nil
}

// GetNumObjectsMatchingSelector returns number of objects matching the given selector.
func GetNumObjectsMatchingSelector(c dynamic.Interface, namespace string, resource schema.GroupVersionResource, labelSelector labels.Selector) (int, error) {
	var numObjects int
	listFunc := func() error {
		list, err := c.Resource(resource).Namespace(namespace).List(metav1.ListOptions{LabelSelector: labelSelector.String()})
		numObjects = len(list.Items)
		return err
	}
	err := client.RetryWithExponentialBackOff(client.RetryFunction(listFunc))
	return numObjects, err
}

// The pod can only schedule onto nodes that satisfy requirements in NodeAffinity.
func podMatchesNodeAffinity(affinity *corev1.Affinity, node *corev1.Node) (bool, error) {
	// 1. nil NodeSelector matches all nodes (i.e. does not filter out any nodes)
	// 2. nil []NodeSelectorTerm (equivalent to non-nil empty NodeSelector) matches no nodes
	// 3. zero-length non-nil []NodeSelectorTerm matches no nodes also, just for simplicity
	// 4. nil []NodeSelectorRequirement (equivalent to non-nil empty NodeSelectorTerm) matches no nodes
	// 5. zero-length non-nil []NodeSelectorRequirement matches no nodes also, just for simplicity
	// 6. non-nil empty NodeSelectorRequirement is not allowed
	if affinity != nil && affinity.NodeAffinity != nil {
		nodeAffinity := affinity.NodeAffinity
		// if no required NodeAffinity requirements, will do no-op, means select all nodes.
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			return true, nil
		}
		nodeSelectorTerms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		matched, err := nodeMatchesNodeSelectorTerms(node, nodeSelectorTerms)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// nodeMatchesNodeSelectorTerms checks if a node's labels satisfy a list of node selector terms,
// terms are ORed, and an empty list of terms will match nothing.
func nodeMatchesNodeSelectorTerms(node *corev1.Node, nodeSelectorTerms []corev1.NodeSelectorTerm) (bool, error) {
	for _, req := range nodeSelectorTerms {
		nodeSelector, err := v1helper.NodeSelectorRequirementsAsSelector(req.MatchExpressions)
		if err != nil {
			return false, err
		}
		if nodeSelector.Matches(labels.Set(node.Labels)) {
			return true, nil
		}
	}
	return false, nil
}
