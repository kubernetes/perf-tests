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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	ns1 = "namespace-1"
)

var (
	daemonsetKind  = appsv1.SchemeGroupVersion.WithKind("DaemonSet")
	replicaSetKind = appsv1.SchemeGroupVersion.WithKind("ReplicaSet")
	deploymentKind = appsv1.SchemeGroupVersion.WithKind("Deployment")
	podKind        = corev1.SchemeGroupVersion.WithKind("Pod")

	daemonset = &appsv1.DaemonSet{
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
	deployment = &appsv1.Deployment{
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

	replicaSet1 = &appsv1.ReplicaSet{
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
	replicaSet2 = &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: replicaSetKind.GroupVersion().String(),
			Kind:       replicaSetKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-2",
			Namespace: ns1,
			UID:       types.UID("uid-7"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, deploymentKind),
			},
		},
	}

	daemonsetPod = &corev1.Pod{
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

	replicaSetPod1 = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-pod-1",
			Namespace: ns1,
			UID:       types.UID("uid-5"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(replicaSet1, replicaSetKind),
			},
		},
	}

	replicaSetPod2 = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-pod-2",
			Namespace: ns1,
			UID:       types.UID("uid-6"),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(replicaSet2, replicaSetKind),
			},
		},
	}
)

func toUnstructured(t *testing.T, obj interface{}) *unstructured.Unstructured {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("failed to convert %T to unstructured: %v", obj, err)
	}
	return &unstructured.Unstructured{Object: content}
}

func newMockedControlledPodsIndexer(ctx context.Context, t *testing.T, client *fake.Clientset) (*ControlledPodsIndexer, error) {
	informerFactory := informers.NewSharedInformerFactory(client, 0 /* resyncPeriod */)
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

	return p, nil
}

func TestControlledPodsIndexer_PodsControlledBy(t *testing.T) {
	var allObjects = []runtime.Object{daemonset, deployment, replicaSet1, replicaSet2, daemonsetPod, replicaSetPod1, replicaSetPod2}
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
			obj:  replicaSet1,
			want: []*corev1.Pod{replicaSetPod1},
		},
		{
			name: "deployment",
			obj:  deployment,
			want: []*corev1.Pod{replicaSetPod1, replicaSetPod2},
		},
		{
			// When we fetch objects using dynamic informer, objects are returend as unstructured.
			// We expect result to be the same as for static-typed deployment.
			name: "deployment as unstructured",
			obj:  toUnstructured(t, deployment),
			want: []*corev1.Pod{replicaSetPod1, replicaSetPod2},
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
			p, err := newMockedControlledPodsIndexer(ctx, t, fakeClient)
			if err != nil {
				t.Fatalf("failed to create ControlledPodsIndexer instance: %v", err)
			}

			got, err := p.PodsControlledBy(tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("PodsIndexer.PodsControlledBy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.ElementsMatch(t, got, tt.want)
		})
	}
}

func TestControlledPodsIndexer_PodsControlledBy_ReplicasetDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fake.NewSimpleClientset(deployment, replicaSet1, replicaSetPod1)
	p, err := newMockedControlledPodsIndexer(ctx, t, fakeClient)
	if err != nil {
		t.Fatalf("failed to create ControlledPodsIndexer instance: %v", err)
	}

	if err := fakeClient.AppsV1().Deployments(ns1).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during deployment deletion: %v", err)
	}
	if err := fakeClient.AppsV1().ReplicaSets(ns1).Delete(ctx, replicaSet1.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during replicaset deletion: %v", err)
	}

	// Sleeping in order for the replicaset informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	_, exists, err := p.rsIndexer.GetByKey(string(replicaSet1.UID))
	if err != nil {
		t.Fatalf("unexpected error while getting replicaset: %v", err)
	}

	if !exists {
		t.Errorf("expected replicaSet to exists in store at this point, but exists=%v", exists)
	}

	want := []*corev1.Pod{replicaSetPod1}
	got, err := p.PodsControlledBy(deployment)
	if err != nil {
		t.Errorf("PodsIndexer.PodsControlledBy() error = %v, wantErr %v", err, nil)
		return
	}

	assert.ElementsMatch(t, got, want)

	if err := fakeClient.CoreV1().Pods(ns1).Delete(ctx, replicaSetPod1.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during pod deletion: %v", err)
	}

	// Sleeping in order for the pod informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	_, exists, err = p.rsIndexer.GetByKey(string(replicaSet1.UID))
	if err != nil {
		t.Fatalf("unexpected error while getting replicaset: %v", err)
	}

	if exists {
		t.Errorf("expected replicaSet to be deleted in store at this point, but exists=%v", exists)
	}
}

func TestControlledPodsIndexer_PodsControlledBy_PodUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fake.NewSimpleClientset(deployment, replicaSet1, replicaSetPod1)
	p, err := newMockedControlledPodsIndexer(ctx, t, fakeClient)
	if err != nil {
		t.Fatalf("failed to create ControlledPodsIndexer instance: %v", err)
	}

	if err := fakeClient.AppsV1().Deployments(ns1).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during deployment deletion: %v", err)
	}
	if err := fakeClient.AppsV1().ReplicaSets(ns1).Delete(ctx, replicaSet1.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during replicaset deletion: %v", err)
	}

	// Sleeping in order for the replicaset informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	changedReplicaSetPod := replicaSetPod1.DeepCopy()
	changedReplicaSetPod.Status.Phase = "Running"
	if _, err := fakeClient.CoreV1().Pods(ns1).Update(ctx, changedReplicaSetPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("unexpected error during pod update: %v", err)
	}

	// Sleeping in order for the pod informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	want := []*corev1.Pod{changedReplicaSetPod.DeepCopy()}
	got, err := p.PodsControlledBy(deployment)
	if err != nil {
		t.Errorf("PodsIndexer.PodsControlledBy() error = %v, wantErr %v", err, nil)
		return
	}

	assert.ElementsMatch(t, got, want)
}

func TestControlledPodsIndexer_PodsControlledBy_PodUpdateUID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fakeClient := fake.NewSimpleClientset(deployment, replicaSet1, replicaSetPod1)
	p, err := newMockedControlledPodsIndexer(ctx, t, fakeClient)
	if err != nil {
		t.Fatalf("failed to create ControlledPodsIndexer instance: %v", err)
	}

	if err := fakeClient.AppsV1().Deployments(ns1).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during deployment deletion: %v", err)
	}
	if err := fakeClient.AppsV1().ReplicaSets(ns1).Delete(ctx, replicaSet1.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("unexpected error during replicaset deletion: %v", err)
	}

	// Sleeping in order for the replicaset informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	changedReplicaSetPod := replicaSetPod1.DeepCopy()
	changedReplicaSetPod.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
		{
			UID: "some-other-uid",
		},
	}
	if _, err := fakeClient.CoreV1().Pods(ns1).Update(ctx, changedReplicaSetPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("unexpected error during pod update: %v", err)
	}

	// Sleeping in order for the pod informer to catch up with the changes.
	time.Sleep(1 * time.Second)

	want := []*corev1.Pod{}
	got, err := p.PodsControlledBy(deployment)
	if err != nil {
		t.Errorf("PodsIndexer.PodsControlledBy() error = %v, wantErr %v", err, nil)
		return
	}

	assert.ElementsMatch(t, got, want)

	_, exists, err := p.rsIndexer.GetByKey(string(replicaSet1.UID))
	if err != nil {
		t.Fatalf("unexpected error while getting replicaset: %v", err)
	}

	if exists {
		t.Errorf("expected replicaSet to be deleted in store at this point, but exists=%v", exists)
	}
}
