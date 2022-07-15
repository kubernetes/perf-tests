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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerUIDIndex = "controllerUID"
)

type ControlledPodsIndexer struct {
	podsIndexer cache.Indexer
	podsSynced  cache.InformerSynced

	rsIndexer cache.Indexer
	rsSynced  cache.InformerSynced
}

func controllerUIDIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("object has no meta: %v", err)
	}
	controllerRef := metav1.GetControllerOf(meta)
	if controllerRef == nil {
		return []string{}, nil
	}
	return []string{string(controllerRef.UID)}, nil
}

func NewControlledPodsIndexer(podsInformer coreinformers.PodInformer, rsInformer appsinformers.ReplicaSetInformer) *ControlledPodsIndexer {
	podsInformer.Informer().AddIndexers(cache.Indexers{controllerUIDIndex: controllerUIDIndexFunc})
	rsInformer.Informer().AddIndexers(cache.Indexers{controllerUIDIndex: controllerUIDIndexFunc})

	return &ControlledPodsIndexer{
		podsIndexer: podsInformer.Informer().GetIndexer(),
		podsSynced:  podsInformer.Informer().HasSynced,
		rsIndexer:   rsInformer.Informer().GetIndexer(),
		rsSynced:    rsInformer.Informer().HasSynced,
	}
}
func (p *ControlledPodsIndexer) WaitForCacheSync(ctx context.Context) bool {
	return cache.WaitForNamedCacheSync("PodsIndexer", ctx.Done(), p.podsSynced, p.podsSynced)
}

func (p *ControlledPodsIndexer) PodsControlledBy(obj interface{}) ([]*corev1.Pod, error) {
	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("object has no meta: %w", err)
	}
	typeAccessor, err := meta.TypeAccessor(obj)
	if err != nil {
		return nil, fmt.Errorf("object has unknown type: %w", err)
	}
	res, err := p.appendPodsControlledBy(nil, metaAccessor.GetUID())
	if err != nil {
		return nil, fmt.Errorf("failed to get pods controlled by %v: %w", metaAccessor.GetUID(), err)
	}

	// Deployments use indirect ownership:
	// Pods -- ownerReference --> ReplicaSet -- ownerReference --> Deployment.
	// We want to also support this case.
	if typeAccessor.GetKind() == "Deployment" {
		replicaSets, err := p.rsIndexer.ByIndex(controllerUIDIndex, string(metaAccessor.GetUID()))
		if err != nil {
			return nil, fmt.Errorf("failed to get replicasets controlled by %v: %w", metaAccessor.GetUID(), err)
		}

		for _, replicaSet := range replicaSets {
			replicaSet, ok := replicaSet.(*appsv1.ReplicaSet)
			if !ok {
				return nil, fmt.Errorf("expected *appsv1.ReplicaSet; got: %T", replicaSet)
			}
			res, err = p.appendPodsControlledBy(res, replicaSet.GetUID())
			if err != nil {
				return nil, fmt.Errorf("failed to get pods controlled by replicaset %v: %w", replicaSet.GetUID(), err)
			}
		}
	}

	return res, nil
}

func (p *ControlledPodsIndexer) appendPodsControlledBy(in []*corev1.Pod, uid types.UID) ([]*corev1.Pod, error) {
	objs, err := p.podsIndexer.ByIndex(controllerUIDIndex, string(uid))
	if err != nil {
		return nil, fmt.Errorf("ByIndex failed: %w", err)
	}

	var res []*corev1.Pod
	for _, obj := range objs {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil, fmt.Errorf("expected *corev1.Pod; got: %T", obj)
		}
		res = append(res, pod)
	}
	return res, nil
}
