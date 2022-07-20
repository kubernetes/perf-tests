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
	"strings"
	"testing"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
)

var (
	controllerName         = "foobar"
	testNamespace          = "test-namespace"
	defaultResourceVersion = "1"
	defaultReplicas        = int32(10)
	daemonsetReplicas      = int32(1)
)

var (
	simpleLabel   = map[string]string{"foo": "bar"}
	affinityLabel = map[string]string{"foo": "bar", "affinity": "true"}
	image         = "gcr.io/some-project/some-image"
)

var node1 = corev1.Node{
	ObjectMeta: metav1.ObjectMeta{
		Name:   "node1",
		Labels: simpleLabel,
	},
}

var node2 = corev1.Node{
	ObjectMeta: metav1.ObjectMeta{
		Name:   "node2",
		Labels: affinityLabel,
	},
}

var affinity = &corev1.Affinity{
	NodeAffinity: &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "affinity",
							Operator: v1.NodeSelectorOpIn,
							Values:   []string{"ok", "true"},
						},
					},
				},
			},
		},
	},
}

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
			Spec: resourcePodSpec("", "50M", "0.5", nil, nil),
		},
	},
}

var replicaset = &apps.ReplicaSet{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName,
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: apps.ReplicaSetSpec{
		Replicas: &defaultReplicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: simpleLabel,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: simpleLabel,
			},
			Spec: resourcePodSpec("", "50M", "0.5", nil, nil),
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
			Spec: resourcePodSpec("", "50M", "0.5", nil, nil),
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
			Spec: resourcePodSpec("", "50M", "0.5", simpleLabel, affinity),
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
			Spec: resourcePodSpec("", "50M", "0.5", nil, nil),
		},
	},
}

// pod is a sample pod that can be created for replicationcontroller,
// replicaset, deployment, job (NOT daemonset).
var pod = &corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName + "-abcd",
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: alterPodSpec(resourcePodSpec("", "50M", "0.5", nil, nil)),
}

var daemonsetPod = &corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name:            controllerName + "-abcd",
		Namespace:       testNamespace,
		ResourceVersion: defaultResourceVersion,
	},
	Spec: alterPodSpec(resourcePodSpec("", "50M", "0.5", simpleLabel, affinity)),
}

func resourcePodSpec(nodeName, memory, cpu string, nodeSelector map[string]string, affinity *v1.Affinity) v1.PodSpec {
	return v1.PodSpec{
		NodeName: nodeName,
		Containers: []v1.Container{{
			Resources: v1.ResourceRequirements{
				Requests: allocatableResources(memory, cpu),
			},
			Image: image,
			Env: []v1.EnvVar{
				{
					Name:  "env1",
					Value: "val1",
				},
			},
		}},
		NodeSelector: nodeSelector,
		Affinity:     affinity,
		Tolerations: []v1.Toleration{
			{
				Key:    "default-toleration",
				Value:  "default-value",
				Effect: v1.TaintEffectNoSchedule,
			},
		},
	}
}

// alterPodSpec changees podSpec to simulate possible differences between template and final pod.
func alterPodSpec(in v1.PodSpec) v1.PodSpec {
	out := in.DeepCopy()
	// append some tolerations
	out.Tolerations = append(out.Tolerations, v1.Toleration{
		Key:    "test",
		Value:  "value",
		Effect: v1.TaintEffectNoExecute,
	})
	// set some defaults
	i := int64(30)
	out.TerminationGracePeriodSeconds = &i
	out.ActiveDeadlineSeconds = &i

	// Simulate schedule
	if out.NodeName == "" {
		out.NodeName = node1.Name
	}

	// Copy resources
	for i := range out.Containers {
		c := &out.Containers[i]
		if c.Resources.Requests == nil {
			c.Resources.Requests = c.Resources.Limits.DeepCopy()
		}
	}
	return *out
}

func allocatableResources(memory, cpu string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceMemory: resource.MustParse(memory),
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourcePods:   resource.MustParse("100"),
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

func changeImage(in *v1.Pod) *v1.Pod {
	out := in.DeepCopy()

	for i := range out.Spec.Containers {
		c := &out.Spec.Containers[i]
		c.Image = c.Image + "-diff"
	}

	return out
}

func changeEnv(in *v1.Pod) *v1.Pod {
	out := in.DeepCopy()

	for i := range out.Spec.Containers {
		c := &out.Spec.Containers[i]
		for j := range c.Env {
			e := &c.Env[j]
			e.Value = e.Value + "-diff"
		}
	}

	return out
}

func noWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, " ", "") //non-breaking space
	return strings.ReplaceAll(s, "\t", "")
}

const fullCompareErrorDifferentEnv = `Not matching templates, diff:   v1.PodSpec{
    Volumes:        nil,
    InitContainers: nil,
    Containers: []v1.Container{
            {
                    ... // 5 identical fields
                    Ports:   nil,
                    EnvFrom: nil,
                    Env: []v1.EnvVar{
                            {
                                    Name:      "env1",
-                                   Value:     "val1",
+                                   Value:     "val1-diff",
                                    ValueFrom: nil,
                            },
                    },
                    Resources:    {Requests: {s"cpu": {i: {...}, Format: "DecimalSI"}, s"memory": {i: {...}, s: "50M", Format: "DecimalSI"}, s"pods": {i: {...}, s: "100", Format: "DecimalSI"}}},
                    VolumeMounts: nil,
                    ... // 12 identical fields
            },
    },
    EphemeralContainers:           nil,
    RestartPolicy:                 "",
-   TerminationGracePeriodSeconds: nil,
+   TerminationGracePeriodSeconds: &30,
-   ActiveDeadlineSeconds:         nil,
+   ActiveDeadlineSeconds:         &30,
    DNSPolicy:                     "",
    NodeSelector:                  nil,
    ServiceAccountName:            "",
    DeprecatedServiceAccount:      "",
    AutomountServiceAccountToken:  nil,
-   NodeName:                      "",
+   NodeName:                      "node1",
    HostNetwork:                   false,
    HostPID:                       false,
    ... // 6 identical fields
    Affinity:      nil,
    SchedulerName: "",
    Tolerations: []v1.Toleration{
            {Key: "default-toleration", Value: "default-value", Effect: "NoSchedule"},
+           {Key: "test", Value: "value", Effect: "NoExecute"},
    },
    HostAliases:       nil,
    PriorityClassName: "",
    ... // 9 identical fields
  }
`

const fullCompareErrorDifferentImage = `Not matching templates, diff:   v1.PodSpec{
    Volumes:        nil,
    InitContainers: nil,
    Containers: []v1.Container{
            {
                    Name:    "",
-                   Image:   "gcr.io/some-project/some-image",
+                   Image:   "gcr.io/some-project/some-image-diff",
                    Command: nil,
                    Args:    nil,
                    ... // 18 identical fields
            },
    },
    EphemeralContainers:           nil,
    RestartPolicy:                 "",
-   TerminationGracePeriodSeconds: nil,
+   TerminationGracePeriodSeconds: &30,
-   ActiveDeadlineSeconds:         nil,
+   ActiveDeadlineSeconds:         &30,
    DNSPolicy:                     "",
    NodeSelector:                  nil,
    ServiceAccountName:            "",
    DeprecatedServiceAccount:      "",
    AutomountServiceAccountToken:  nil,
-   NodeName:                      "",
+   NodeName:                      "node1",
    HostNetwork:                   false,
    HostPID:                       false,
    ... // 6 identical fields
    Affinity:      nil,
    SchedulerName: "",
    Tolerations: []v1.Toleration{
            {Key: "default-toleration", Value: "default-value", Effect: "NoSchedule"},
+           {Key: "test", Value: "value", Effect: "NoExecute"},
    },
    HostAliases:       nil,
    PriorityClassName: "",
    ... // 9 identical fields
  }`

func TestGetIsPodUpdatedPredicateFromRuntimeObject(t *testing.T) {
	testCases := []struct {
		name                 string
		obj                  runtime.Object
		pod                  *corev1.Pod
		wantErr              bool
		wantfullCompareError string
	}{
		{
			name: "deployment, positive",
			obj:  deployment,
			pod:  pod,
		},
		{
			name:                 "deployment, different env",
			obj:                  deployment,
			pod:                  changeEnv(pod),
			wantfullCompareError: fullCompareErrorDifferentEnv,
		},
		{
			name:                 "deployment, different image",
			obj:                  deployment,
			pod:                  changeImage(pod),
			wantfullCompareError: fullCompareErrorDifferentImage,
		},
		{
			name: "replicaset, positive",
			obj:  replicaset,
			pod:  pod,
		},
		{
			name: "replicationcontroller, positive",
			obj:  replicationcontroller,
			pod:  pod,
		},
		{
			name: "daemonset, positive",
			obj:  daemonset,
			pod:  daemonsetPod,
		},
		{
			name: "job, positive",
			obj:  job,
			pod:  pod,
		},
		{
			name:    "no spec.template",
			obj:     pod, // pod has no spec.template field.
			pod:     pod,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			unstructured := &unstructured.Unstructured{}
			if err := scheme.Scheme.Convert(tc.obj, unstructured, nil); err != nil {
				t.Fatalf("error converting controller to unstructured: %v", err)
			}
			pred, err := runtimeobjects.GetIsPodUpdatedPredicateFromRuntimeObject(unstructured)
			if (err != nil) != tc.wantErr {
				t.Errorf("unexpected error; want: %v; got %v", tc.wantErr, err)
			}
			if err != nil {
				return
			}
			gotErr := pred(tc.pod)
			var gotErrString string
			if gotErr != nil {
				gotErrString = gotErr.Error()
			}
			if noWhitespace(gotErrString) != noWhitespace(tc.wantfullCompareError) {
				t.Errorf("pred(tc.pod) = %v; want %v", noWhitespace(gotErrString), noWhitespace(tc.wantfullCompareError))
			}
		})
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
		daemonsetReplicas,
	}

	fakeClient := fake.NewSimpleClientset(&node1, &node2)
	for i, obj := range objects {
		unstructured := &unstructured.Unstructured{}
		if err := scheme.Scheme.Convert(obj, unstructured, nil); err != nil {
			t.Fatalf("error converting controller to unstructured: %v", err)
		}
		replicasWatcher, err := runtimeobjects.GetReplicasFromRuntimeObject(fakeClient, unstructured)
		if err != nil {
			t.Fatalf("get replicas from runtime object failed: %v", err)
		}

		replicas, err := runtimeobjects.GetReplicasOnce(replicasWatcher)
		if err != nil {
			t.Fatalf("got unexpected error while getting number of replicas: %v", err)
		}

		if int(expected[i]) != replicas {
			t.Fatalf("Unexpected replicas from runtime object, expected: %d, actual: %d", expected[i], replicas)
		}
	}
}
