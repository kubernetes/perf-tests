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
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	controllerUIDIndex = "controllerUID"
)

// ControlledPodsIndexer is able to efficiently find pods with ownerReference pointing a given controller object.
// For Deployments, it performs indirect lookup with ReplicaSets in the middle.
type ControlledPodsIndexer struct {
	podsIndexer cache.Indexer
	podsSynced  cache.InformerSynced
	rsSynced    cache.InformerSynced

	rsIndexer cache.Indexer

	// rsUIDToState is a map between a ReplicaSet object's UID and its state.
	rsUIDToState map[types.UID]*ReplicaSetState

	// UIDLock is a lock for accessing rsUIDToState and rsIndexer fields in ControlledPodsIndexer.
	// NOTE: rsIndexer is already thread-safe, so lock here is slightly redundant. We use it to make sure that
	// state in rsUIDToState matches rsIndexer.
	UIDLock sync.Mutex
}

// ReplicaSetState stores information relevant to a specific ReplicaSet object,
// i.e. how many pods it owns exist, whether the RS object itself exists
// and its latest known owner's UID.
type ReplicaSetState struct {
	NumPods int
	Exists  bool
}

// UIDSet is a collection of ReplicaSet objects UIDs.
type UIDSet map[types.UID]bool

func deletionHandlingUIDKeyFunc(obj interface{}) (string, error) {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return d.Key, nil
	}
	return string(getObjUID(obj)), nil
}

// NewControlledPodsIndexer creates a new ControlledPodsIndexer instance.
func NewControlledPodsIndexer(podsInformer coreinformers.PodInformer, rsInformer appsinformers.ReplicaSetInformer) (*ControlledPodsIndexer, error) {
	if err := podsInformer.Informer().AddIndexers(cache.Indexers{controllerUIDIndex: controllerUIDIndexFunc}); err != nil {
		return nil, fmt.Errorf("failed to register indexer: %w", err)
	}

	// We need a separate storage from rsInformer as we postpone deletion until all pods are removed.
	rsIndexer := cache.NewIndexer(deletionHandlingUIDKeyFunc, cache.Indexers{controllerUIDIndex: controllerUIDIndexFunc})

	cpi := &ControlledPodsIndexer{
		podsIndexer:  podsInformer.Informer().GetIndexer(),
		podsSynced:   podsInformer.Informer().HasSynced,
		rsIndexer:    rsIndexer,
		rsSynced:     rsInformer.Informer().HasSynced,
		rsUIDToState: make(map[types.UID]*ReplicaSetState),
	}

	podsInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()

				ownerUID, ownerKind := getControllerInfo(obj)
				if ownerUID == "" || ownerKind != "ReplicaSet" {
					return
				}

				cpi.updatePodRefCountLocked(ownerUID, 1)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()

				oldOwnerUID, oldOwnerKind := getControllerInfo(oldObj)
				newOwnerUID, newOwnerKind := getControllerInfo(newObj)
				if oldOwnerUID == newOwnerUID {
					return
				}

				if oldOwnerUID != "" && oldOwnerKind == "ReplicaSet" {
					cpi.updatePodRefCountLocked(oldOwnerUID, -1)
					if err := cpi.clearRSDataIfPossibleLocked(oldOwnerUID); err != nil {
						klog.Errorf("error while deleting %v: %v", oldOwnerUID, err)
					}
				}
				if newOwnerUID != "" && newOwnerKind == "ReplicaSet" {
					cpi.updatePodRefCountLocked(newOwnerUID, 1)
				}
			},
			DeleteFunc: func(obj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()

				ownerUID, ownerKind := getControllerInfo(obj)
				if ownerUID == "" || ownerKind != "ReplicaSet" {
					return
				}

				cpi.updatePodRefCountLocked(ownerUID, -1)
				if err := cpi.clearRSDataIfPossibleLocked(ownerUID); err != nil {
					klog.Errorf("error while deleting %v: %v", ownerUID, err)
				}
			},
		},
	)
	rsInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()

				if err := cpi.handleIncomingRSAddEventLocked(obj); err != nil {
					klog.Errorf("error while adding %v: %v", obj, err)
				}
			},
			UpdateFunc: func(_, newObj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()

				if err := rsIndexer.Update(newObj); err != nil {
					klog.Errorf("error while updating %v: %v", newObj, err)
				}
			},
			DeleteFunc: func(obj interface{}) {
				cpi.UIDLock.Lock()
				defer cpi.UIDLock.Unlock()
				rsUID := getObjUID(obj)
				cpi.rsUIDToState[rsUID].Exists = false
				if err := cpi.clearRSDataIfPossibleLocked(rsUID); err != nil {
					klog.Errorf("error while deleting %v: %v", obj, err)
				}
			},
		},
	)

	return cpi, nil
}

func getControllerInfo(obj interface{}) (types.UID, string) {
	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		return "", ""
	}
	controller := metav1.GetControllerOf(metaAccessor)
	if controller == nil {
		return "", ""
	}
	return controller.UID, controller.Kind
}

func getObjUID(obj interface{}) types.UID {
	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		return ""
	}
	return metaAccessor.GetUID()
}

func (p *ControlledPodsIndexer) clearRSDataIfPossibleLocked(rsUID types.UID) error {
	state := p.rsUIDToState[rsUID]
	if state != nil && !state.Exists && state.NumPods == 0 {
		delete(p.rsUIDToState, rsUID)

		obj, exists, err := p.rsIndexer.GetByKey(string(rsUID))
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}

		return p.rsIndexer.Delete(obj)
	}
	return nil
}

func (p *ControlledPodsIndexer) updatePodRefCountLocked(rsUID types.UID, diff int) {
	if _, ok := p.rsUIDToState[rsUID]; !ok {
		p.rsUIDToState[rsUID] = &ReplicaSetState{}
	}
	p.rsUIDToState[rsUID].NumPods += diff
}

func (p *ControlledPodsIndexer) handleIncomingRSAddEventLocked(obj interface{}) error {
	rsUID := getObjUID(obj)
	if _, ok := p.rsUIDToState[rsUID]; !ok {
		p.rsUIDToState[rsUID] = &ReplicaSetState{}
	}
	p.rsUIDToState[rsUID].Exists = true
	return p.rsIndexer.Add(obj)
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

// WaitForCacheSync waits for all required informers to be initialized.
func (p *ControlledPodsIndexer) WaitForCacheSync(ctx context.Context) bool {
	return cache.WaitForNamedCacheSync("PodsIndexer", ctx.Done(), p.podsSynced, p.rsSynced)
}

// PodsControlledBy returns pods controlled by a given controller object.
func (p *ControlledPodsIndexer) PodsControlledBy(obj interface{}) ([]*corev1.Pod, error) {
	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("object has no meta: %w", err)
	}
	typeAccessor, err := meta.TypeAccessor(obj)
	if err != nil {
		return nil, fmt.Errorf("object has unknown type: %w", err)
	}

	var podOwners []types.UID
	switch typeAccessor.GetKind() {
	case "Deployment":
		p.UIDLock.Lock()
		replicaSets, err := p.rsIndexer.ByIndex(controllerUIDIndex, string(metaAccessor.GetUID()))
		if err != nil {
			return nil, fmt.Errorf("failed to get replicasets controlled by %v: %w", metaAccessor.GetUID(), err)
		}
		for _, replicaSet := range replicaSets {
			replicaSet, ok := replicaSet.(*appsv1.ReplicaSet)
			if !ok {
				return nil, fmt.Errorf("expected *appsv1.ReplicaSet; got: %T", replicaSet)
			}
			podOwners = append(podOwners, replicaSet.GetUID())
		}
		p.UIDLock.Unlock()
	default:
		podOwners = append(podOwners, metaAccessor.GetUID())
	}

	var res []*corev1.Pod
	for _, podOwner := range podOwners {
		res, err = p.appendPodsControlledBy(res, podOwner)
		if err != nil {
			return nil, fmt.Errorf("failed to get pods controlled by %v: %w", podOwner, err)
		}
	}

	return res, nil
}

func (p *ControlledPodsIndexer) appendPodsControlledBy(in []*corev1.Pod, uid types.UID) ([]*corev1.Pod, error) {
	objs, err := p.podsIndexer.ByIndex(controllerUIDIndex, string(uid))
	if err != nil {
		return nil, fmt.Errorf("method ByIndex failed: %w", err)
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
