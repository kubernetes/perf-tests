/*
Copyright 2019 The Kubernetes Authors.

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

package informer

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
)

// NewInformer creates a new informer.
func NewInformer(
	lw cache.ListerWatcher,
	handleObj func(interface{}, interface{}),
) cache.SharedInformer {
	informer := cache.NewSharedInformer(lw, nil, 0)
	addEventHandler(informer, handleObj)
	return informer
}

// NewDynamicInformer creates a new dynamic informer
// for given namespace, fieldSelector and labelSelector.
func NewDynamicInformer(
	c dynamic.Interface,
	gvr schema.GroupVersionResource,
	selector *measurementutil.ObjectSelector,
	handleObj func(interface{}, interface{}),
) cache.SharedInformer {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = selector.FieldSelector
		options.LabelSelector = selector.LabelSelector
	}
	tweakListOptions := dynamicinformer.TweakListOptionsFunc(optionsModifier)
	dInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(c, 0, selector.Namespace, tweakListOptions)

	informer := dInformerFactory.ForResource(gvr).Informer()
	addEventHandler(informer, handleObj)
	return informer
}

func addEventHandler(i cache.SharedInformer,
	handleObj func(interface{}, interface{}),
) {
	i.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleObj(nil, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handleObj(oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				handleObj(tombstone.Obj, nil)
			} else {
				handleObj(obj, nil)
			}
		},
	})
}

// StartAndSync starts informer and waits for it to be synced.
func StartAndSync(i cache.SharedInformer, stopCh <-chan struct{}, timeout time.Duration) error {
	go i.Run(stopCh)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(timeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, i.HasSynced) {
		return fmt.Errorf("timed out waiting for caches to sync")
	}
	return nil
}
