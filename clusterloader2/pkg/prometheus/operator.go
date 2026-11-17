/*
Copyright The Kubernetes Authors.

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

package prometheus

import (
	"context"
	"fmt"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	operatorPodSelector   = "app.kubernetes.io/name=prometheus-operator"
	prometheusStatefulSet = "prometheus-k8s"
	// The readiness poll runs every 30 seconds, so the operator gets a minute.
	operatorMissingChecksBeforeRestart = 3
	operatorRestartLimit               = 5
)

// operatorWatchdog restarts prometheus-operator when it has not created the
// Prometheus StatefulSet.
//
// The operator looks for its CRDs once, at startup. With several API servers
// it can ask one that does not serve the new CRDs yet. It then runs without
// its Prometheus controller and never creates the StatefulSet.
type operatorWatchdog struct {
	missing  int
	restarts int
}

// check is called once per readiness poll.
func (w *operatorWatchdog) check(ctx context.Context, c clientset.Interface) error {
	_, err := c.AppsV1().StatefulSets(namespace).Get(ctx, prometheusStatefulSet, metav1.GetOptions{})
	if err == nil {
		w.missing = 0
		return nil
	}
	if !apierrs.IsNotFound(err) {
		return fmt.Errorf("getting StatefulSet %s/%s: %w", namespace, prometheusStatefulSet, err)
	}
	w.missing++
	if w.missing < operatorMissingChecksBeforeRestart || w.restarts == operatorRestartLimit {
		return nil
	}
	klog.Warningf("StatefulSet %s/%s is missing, restarting prometheus-operator", namespace, prometheusStatefulSet)
	if err := c.CoreV1().Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: operatorPodSelector}); err != nil {
		return fmt.Errorf("restarting prometheus-operator: %w", err)
	}
	w.missing = 0
	w.restarts++
	return nil
}
