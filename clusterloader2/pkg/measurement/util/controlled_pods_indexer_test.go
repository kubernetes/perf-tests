/*
Copyright 2022 The Kubernetes Authors.

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

package util

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	daemonsetKind  = appsv1.SchemeGroupVersion.WithKind("DaemonSet")
	replicaSetKind = appsv1.SchemeGroupVersion.WithKind("ReplicaSet")
	deploymentKind = appsv1.SchemeGroupVersion.WithKind("Deployment")
	podKind        = corev1.SchemeGroupVersion.WithKind("Pod")
)

func toUnstructured(t *testing.T, obj interface{}) *unstructured.Unstructured {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("failed to convert %T to unstructured: %v", obj, err)
	}
	return &unstructured.Unstructured{Object: content}
}

func TestControlledPodsIndexer_PodsControlledBy(t *testing.T) {
	const ns1 = "namespace-1"
	daemonset := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: daemonsetKind.GroupVersion().String(),
			Kind:       daemonsetKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "daemonset-1",
			Namespace: ns1,
			UID:       types.UID("uid-1"),
		},
	}
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: deploymentKind.GroupVersion().String(),
			Kind:       deploymentKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-1",
			Namespace: ns1,
			UID:       types.UID("uid-2"),
		},
	}

	replicaSet := &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: replicaSetKind.GroupVersion().String(),
			Kind:       replicaSetKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-1",
			Namespace: ns1,
			UID:       types.UID("uid-3"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, deploymentKind),
			},
		},
	}

	daemonsetPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: podKind.GroupVersion().String(),
			Kind:       podKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: ns1,
			UID:       types.UID("uid-4"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(daemonset, daemonsetKind),
			},
		},
	}

	replicaSetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: ns1,
			UID:       types.UID("uid-5"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(replicaSet, replicaSetKind),
			},
		},
	}

	var allObjects = []runtime.Object{daemonset, deployment, replicaSet, daemonsetPod, replicaSetPod}
	tests := []struct {
		name    string
		obj     interface{}
		want    []*corev1.Pod
		wantErr bool
	}{
		{
			name: "daemonset",
			obj:  daemonset,
			want: []*corev1.Pod{daemonsetPod},
		},
		{
			name: "replicaset",
			obj:  replicaSet,
			want: []*corev1.Pod{replicaSetPod},
		},
		{
			name: "deployment",
			obj:  deployment,
			want: []*corev1.Pod{replicaSetPod},
		},
		{
			// When we fetch objects using dynamic informer, objects are returend as unstructured.
			// We expect result to be the same as for static-typed deployment.
			name: "deployment as unstructured",
			obj:  toUnstructured(t, deployment),
			want: []*corev1.Pod{replicaSetPod},
		},
		{
			name: "daemonset as unstructured",
			obj:  toUnstructured(t, daemonset),
			want: []*corev1.Pod{daemonsetPod},
		},
		{
			name:    "unknown type",
			obj:     "string doesn't implement metav1.Object",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeClient := fake.NewSimpleClientset(allObjects...)
			informerFactory := informers.NewSharedInformerFactory(fakeClient, 0 /* resyncPeriod */)
			podsInformer := informerFactory.Core().V1().Pods()
			rsInformer := informerFactory.Apps().V1().ReplicaSets()
			p, err := NewControlledPodsIndexer(podsInformer, rsInformer)
			if err != nil {
				t.Fatalf("failed to create ControlledPodsIndexer instance: %v", err)
			}
			informerFactory.Start(ctx.Done())
			if !p.WaitForCacheSync(ctx) {
				t.Fatalf("failed to sync informer")
			}

			got, err := p.PodsControlledBy(tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("PodsIndexer.PodsControlledBy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PodsIndexer.PodsControlledBy() = %v, want %v", got, tt.want)
			}
		})
	}
}
