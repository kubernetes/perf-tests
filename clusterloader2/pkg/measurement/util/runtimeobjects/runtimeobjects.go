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
	"context"
	"fmt"
	"strconv"

	goerrors "github.com/go-errors/errors"
	gocmp "github.com/google/go-cmp/cmp"
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
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	v1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
	dsutil "k8s.io/kubernetes/pkg/controller/daemon/util"
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
		list, err := d.Resource(gvr).List(context.TODO(), listOpts)
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

// GetIsPodUpdatedPredicateFromRuntimeObject returns a func(*corev1.Pod) bool predicate
// that can be used to check if given pod represents the desired state of pod.
func GetIsPodUpdatedPredicateFromRuntimeObject(obj runtime.Object) (func(*corev1.Pod) error, error) {
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getIsPodUpdatedPodPredicateFromUnstructured(typed)
	default:
		return nil, goerrors.Errorf("unsupported kind when getting updated pod predicate: %v", obj)
	}
}

// Auxiliary error type for lazy evaluation of gocmp.Diff which is known to be
// computationally expensive.
type lazySpecDiffError struct {
	templateSpec corev1.PodSpec
	podSpec      corev1.PodSpec
}

func (lsde *lazySpecDiffError) Error() string {
	return fmt.Sprintf("Not matching templates, diff: %v", gocmp.Diff(lsde.templateSpec, lsde.podSpec))
}

func getIsPodUpdatedPodPredicateFromUnstructured(obj *unstructured.Unstructured) (func(_ *corev1.Pod) error, error) {
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

	return func(pod *corev1.Pod) error {
		if !equality.Semantic.DeepDerivative(template.Spec, pod.Spec) {
			return &lazySpecDiffError{template.Spec, pod.Spec}
		}
		return nil
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
func GetReplicasFromRuntimeObject(c clientset.Interface, obj runtime.Object) (ReplicasWatcher, error) {
	if obj == nil {
		return &ConstReplicas{0}, nil
	}
	switch typed := obj.(type) {
	case *unstructured.Unstructured:
		return getReplicasFromUnstrutured(c, typed)
	case *corev1.ReplicationController:
		if typed.Spec.Replicas != nil {
			return &ConstReplicas{int(*typed.Spec.Replicas)}, nil
		}
		return &ConstReplicas{0}, nil
	case *appsv1.ReplicaSet:
		if typed.Spec.Replicas != nil {
			return &ConstReplicas{int(*typed.Spec.Replicas)}, nil
		}
		return &ConstReplicas{0}, nil
	case *appsv1.Deployment:
		if typed.Spec.Replicas != nil {
			return &ConstReplicas{int(*typed.Spec.Replicas)}, nil
		}
		return &ConstReplicas{0}, nil
	case *appsv1.StatefulSet:
		if typed.Spec.Replicas != nil {
			return &ConstReplicas{int(*typed.Spec.Replicas)}, nil
		}
		return &ConstReplicas{0}, nil
	case *appsv1.DaemonSet:
		return getDaemonSetNumSchedulableNodes(c, &typed.Spec.Template.Spec)
	case *batch.Job:
		if typed.Spec.Parallelism != nil {
			return &ConstReplicas{int(*typed.Spec.Parallelism)}, nil
		}
		return &ConstReplicas{0}, nil
	default:
		return nil, fmt.Errorf("unsupported kind when getting number of replicas: %v", obj)
	}
}

// getDaemonSetNumSchedulableNodes returns the number of schedulable nodes matching both nodeSelector and NodeAffinity.
func getDaemonSetNumSchedulableNodes(c clientset.Interface, podSpec *corev1.PodSpec) (ReplicasWatcher, error) {
	selector, err := metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(podSpec.NodeSelector))
	if err != nil {
		return nil, err
	}
	return NewNodeCounter(c, selector, podSpec.Affinity, podSpec.Tolerations), nil
}

// Note: This function assumes each controller has field Spec.Replicas, except DaemonSets and Job.
func getReplicasFromUnstrutured(c clientset.Interface, obj *unstructured.Unstructured) (ReplicasWatcher, error) {
	spec, err := getSpecFromUnstrutured(obj)
	if err != nil {
		return nil, err
	}
	return tryAcquireReplicasFromUnstructuredSpec(c, spec, obj.GetKind())
}

func tryAcquireReplicasFromUnstructuredSpec(c clientset.Interface, spec map[string]interface{}, kind string) (ReplicasWatcher, error) {
	switch kind {
	case "DaemonSet":
		parser, err := newDaemonSetPodSpecParser(spec)
		if err != nil {
			return nil, err
		}
		var podSpec corev1.PodSpec
		if err := parser.getDaemonSetNodeSelectorFromUnstructuredSpec(&podSpec); err != nil {
			return nil, err
		}
		if err := parser.getDaemonSetAffinityFromUnstructuredSpec(&podSpec); err != nil {
			return nil, err
		}
		if err := parser.getDaemonSetTolerationsFromUnstructuredSpec(&podSpec); err != nil {
			return nil, err
		}
		return getDaemonSetNumSchedulableNodes(c, &podSpec)
	case "Job":
		replicas, found, err := unstructured.NestedInt64(spec, "parallelism")
		if err != nil {
			return nil, fmt.Errorf("try to acquire job parallelism failed, %v", err)
		}
		if !found {
			return &ConstReplicas{0}, nil
		}
		return &ConstReplicas{int(replicas)}, nil
	default:
		replicas, found, err := unstructured.NestedInt64(spec, "replicas")
		if err != nil {
			return nil, fmt.Errorf("try to acquire replicas failed, %v", err)
		}
		if !found {
			return &ConstReplicas{0}, nil
		}
		return &ConstReplicas{int(replicas)}, nil
	}
}

type daemonSetPodSpecParser map[string]interface{}

func newDaemonSetPodSpecParser(spec map[string]interface{}) (daemonSetPodSpecParser, error) {
	template, found, err := unstructured.NestedMap(spec, "template")
	if err != nil || !found {
		return nil, err
	}
	podSpec, found, err := unstructured.NestedMap(template, "spec")
	if err != nil || !found {
		return nil, err
	}
	return podSpec, nil
}

func (p daemonSetPodSpecParser) getDaemonSetNodeSelectorFromUnstructuredSpec(spec *corev1.PodSpec) error {
	nodeSelector, _, err := unstructured.NestedStringMap(p, "nodeSelector")
	spec.NodeSelector = nodeSelector
	return err
}

func (p daemonSetPodSpecParser) getDaemonSetAffinityFromUnstructuredSpec(spec *corev1.PodSpec) error {
	unstructuredAffinity, found, err := unstructured.NestedMap(p, "affinity")
	if err != nil || !found {
		return err
	}
	affinity := &corev1.Affinity{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredAffinity, affinity)
	spec.Affinity = affinity
	return err
}

func (p daemonSetPodSpecParser) getDaemonSetTolerationsFromUnstructuredSpec(spec *corev1.PodSpec) error {
	dsutil.AddOrUpdateDaemonPodTolerations(spec)
	unstructuredTolerations, found, err := unstructured.NestedSlice(p, "tolerations")
	if err != nil || !found {
		return err
	}
	for _, unstructuredToleration := range unstructuredTolerations {
		var toleration corev1.Toleration
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredToleration.(map[string]interface{}), &toleration)
		if err != nil {
			break
		}
		v1helper.AddOrUpdateTolerationInPodSpec(spec, &toleration)
	}
	return err
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

// GetNumObjectsMatchingSelector returns number of objects matching the given selector.
func GetNumObjectsMatchingSelector(c dynamic.Interface, namespace string, resource schema.GroupVersionResource, labelSelector labels.Selector) (int, error) {
	var numObjects int
	listFunc := func() error {
		list, err := c.Resource(resource).Namespace(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector.String()})
		if err != nil {
			return err
		}
		numObjects = len(list.Items)
		return nil
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
		return corev1helpers.MatchNodeSelectorTerms(node, nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
	}
	return true, nil
}
