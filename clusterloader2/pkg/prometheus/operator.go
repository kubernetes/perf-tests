/*
Copyright 2026 The Kubernetes Authors.

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
	"time"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	operatorPodSelector    = "app.kubernetes.io/name=prometheus-operator"
	prometheusStatefulSet  = "prometheus-k8s"
	operatorRestartLimit   = 5
	operatorCheckInterval  = 5 * time.Second
	operatorCreateDeadline = time.Minute
)

// ensureOperatorCreatedPrometheus waits for the operator to create the
// Prometheus StatefulSet, and restarts the operator when it does not.
//
// The operator looks for its CRDs once, at startup. With several API servers
// it can ask one that does not serve the new CRDs yet. It then runs without
// its Prometheus controller and never creates the StatefulSet.
func ensureOperatorCreatedPrometheus(c clientset.Interface, interval, deadline time.Duration) error {
	for attempt := 0; ; attempt++ {
		err := wait.PollUntilContextTimeout(context.Background(), interval, deadline, true, func(ctx context.Context) (bool, error) {
			_, err := c.AppsV1().StatefulSets(namespace).Get(ctx, prometheusStatefulSet, metav1.GetOptions{})
			if apierrs.IsNotFound(err) {
				return false, nil
			}
			return err == nil, err
		})
		if err == nil {
			return nil
		}
		if !wait.Interrupted(err) {
			return fmt.Errorf("getting StatefulSet %s/%s: %w", namespace, prometheusStatefulSet, err)
		}
		if attempt == operatorRestartLimit {
			return fmt.Errorf("prometheus-operator did not create StatefulSet %s/%s after %d restarts", namespace, prometheusStatefulSet, operatorRestartLimit)
		}
		klog.Warningf("StatefulSet %s/%s not created after %v, restarting prometheus-operator", namespace, prometheusStatefulSet, deadline)
		if err := c.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: operatorPodSelector}); err != nil {
			return fmt.Errorf("restarting prometheus-operator: %w", err)
		}
	}
}
