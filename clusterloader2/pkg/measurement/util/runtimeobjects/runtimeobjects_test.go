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

package runtimeobjects_test

import (
	"fmt"
	"reflect"
	"testing"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
)

var (
	controllerName         = "foobar"
	testNamespace          = "test-namespace"
	defaultResourceVersion = "1"
	defaultReplicas        = int32(10)
)

var (
	simpleLabel = map[string]string{"foo": "bar"}
)

var replicationcontroller = &corev1.ReplicationController{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: corev1.ReplicationControllerSpec{
		Replicas: &defaultReplicas,
		Selector: simpleLabel,
		Template: &v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5"),
		},
	},
}

var replicaset = &extensions.ReplicaSet{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: extensions.ReplicaSetSpec{
		Replicas: &defaultReplicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: simpleLabel,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5"),
		},
	},
}

var deployment = &apps.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: apps.DeploymentSpec{
		Replicas: &defaultReplicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: simpleLabel,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5"),
		},
	},
}

var daemonset = &apps.DaemonSet{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: apps.DaemonSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: simpleLabel,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5"),
		},
	},
}

var job = &batch.Job{
	TypeMeta: metav1.TypeMeta{Kind: "Job"},
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: batch.JobSpec{
		Parallelism: &defaultReplicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: simpleLabel,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5"),
		},
	},
}

func resourcePodSpec(nodeName, memory, cpu string) v1.PodSpec {
	return v1.PodSpec{
		NodeName: nodeName,
		Containers: []v1.Container{{
			Resources: v1.ResourceRequirements{
				Requests: allocatableResources(memory, cpu),
			},
		}},
	}
}

func allocatableResources(memory, cpu string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourcePods:   resource.MustParse("100"),
	}
}

func TestGetNameFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}

	for _, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		name, err := runtimeobjects.GetNameFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get name from runtime object failed: %v", err)
		}

		if controllerName != name {
			t.Fatalf("Unexpected name from runtime object, expected: %s, actual: %s", controllerName, name)
		}
	}
}

func TestGetNamespaceFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}
	for _, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		namespace, err := runtimeobjects.GetNamespaceFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get namespace from runtime object failed: %v", err)
		}

		if testNamespace != namespace {
			t.Fatalf("Unexpected namespace from runtime object, expected: %s, actual: %s", testNamespace, namespace)
		}
	}
}

func TestGetResourceVersionFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}
	for _, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		rv, err := runtimeobjects.GetResourceVersionFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get resource version from runtime object failed: %v", err)
		}

		if defaultResourceVersion != fmt.Sprint(rv) {
			t.Fatalf("Unexpected resource version from runtime object, expected: %s, actual: %v", defaultResourceVersion, rv)
		}
	}
}

func TestGetSelectorFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}

	ps := &metav1.LabelSelector{
		MatchLabels: simpleLabel,
	}
	expected, err := metav1.LabelSelectorAsSelector(ps)
	if err != nil {
		t.Fatalf("create label selector failed: %v", err)
	}
	for _, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		selector, err := runtimeobjects.GetSelectorFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get selector from runtime object failed: %v", err)
		}

		if !reflect.DeepEqual(expected, selector) {
			t.Fatalf("Unexpected selector from runtime object, expected: %d, actual: %d", expected, selector)
		}
	}
}
func TestGetSpecFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}
	expected := []interface{}{
		replicationcontroller.Spec,
		replicaset.Spec,
		deployment.Spec,
		job.Spec,
		daemonset.Spec,
	}
	for i, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		spec, err := runtimeobjects.GetSpecFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get spec from runtime object failed: %v", err)
		}
		target, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&expected[i])
		if err != nil {
			t.Fatalf("error converting target spec to unstructured: %v", err)
		}

		if !reflect.DeepEqual(target, spec) {
			t.Fatalf("Unexpected spec from runtime object, expected: %v, actual: %v", expected[i], spec)
		}
	}
}

func TestGetReplicasFromRuntimeObject(t *testing.T) {
	objects := []runtime.Object{
		replicationcontroller,
		replicaset,
		deployment,
		job,
		daemonset,
	}
	expected := []int32{
		defaultReplicas,
		defaultReplicas,
		defaultReplicas,
		defaultReplicas,
		0,
	}
	for i, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		replicas, err := runtimeobjects.GetReplicasFromRuntimeObject(unstructured)
		if err != nil {
			t.Fatalf("get replicas from runtime object failed: %v", err)
		}

		if expected[i] != replicas {
			t.Fatalf("Unexpected replicas from runtime object, expected: %d, actual: %d", expected[i], replicas)
		}
	}
}
