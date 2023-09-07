/*
Copyright 2023 The Kubernetes Authors.

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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

// WaitForGenericK8sObjectsOptions is an options object used by WaitForGenericK8sObjectsNodes methods.
type WaitForGenericK8sObjectsOptions struct {
	// GroupVersionResource identifies the resource to fetch.
	GroupVersionResource schema.GroupVersionResource
	// Namespaces identifies namespaces which should be observed.
	Namespaces NamespacesRange
	// SuccessfulConditions lists conditions to look for in the objects denoting good objects.
	// Formatted as `ConditionType=ConditionStatus`, e.g. `Scheduled=true`.
	SuccessfulConditions []string
	// SuccessfulConditions lists conditions to look for in the objects denoting good objects.
	// Formatted as `ConditionType=ConditionStatus`, e.g. `Scheduled=true`.
	FailedConditions []string
	// MinDesiredObjectCount describes minimum number of objects that should contain
	// successful or failed condition.
	MinDesiredObjectCount int
	// MaxFailedObjectCount describes maximum number of objects that could contain failed condition.
	MaxFailedObjectCount int
	// CallerName identifies the measurement making the calls.
	CallerName string
	// WaitInterval contains interval for which the function waits between refreshes.
	WaitInterval time.Duration
}

// NamespacesRange represents namespace range which will be queried.
type NamespacesRange struct {
	Prefix string
	Min    int
	Max    int
}

// Summary returns summary which should be included in all logs.
func (o *WaitForGenericK8sObjectsOptions) Summary() string {
	return fmt.Sprintf("%s: objects: %q, namespaces: %q", o.CallerName, o.GroupVersionResource.String(), o.Namespaces.String())
}

// String returns printable representation of the namespaces range.
func (nr *NamespacesRange) String() string {
	return fmt.Sprintf("%s-(%d-%d)", nr.Prefix, nr.Min, nr.Max)
}

// getMap returns a map with namespaces which should be queried.
func (nr *NamespacesRange) getMap() map[string]bool {
	result := map[string]bool{}
	for i := nr.Min; i <= nr.Max; i++ {
		result[fmt.Sprintf("%s-%d", nr.Prefix, i)] = true
	}
	return result
}

// WaitForGenericK8sObjects waits till the desired number of k8s objects
// fulfills given conditions requirements, ctx.Done() channel is used to
// wait for timeout.
func WaitForGenericK8sObjects(ctx context.Context, dynamicClient dynamic.Interface, options *WaitForGenericK8sObjectsOptions) error {
	store, err := NewDynamicObjectStore(ctx, dynamicClient, options.GroupVersionResource, options.Namespaces.getMap())
	if err != nil {
		return err
	}

	conditions, err := store.ListConditions()
	if err != nil {
		return err
	}
	successful, failed, all := countObjectsMatchingConditions(conditions, options.SuccessfulConditions, options.FailedConditions)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: timeout while waiting for %d objects to be successful or failed - currently there are: successful=%d failed=%d all=%d",
				options.Summary(), options.MinDesiredObjectCount, successful, failed, all)
		case <-time.After(options.WaitInterval):
			conditions, err := store.ListConditions()
			if err != nil {
				return err
			}
			successful, failed, all = countObjectsMatchingConditions(conditions, options.SuccessfulConditions, options.FailedConditions)

			klog.V(2).Infof("%s: successful=%d failed=%d all=%d", options.Summary(), successful, failed, all)
			if options.MinDesiredObjectCount <= successful+failed {
				if options.MaxFailedObjectCount < failed {
					return fmt.Errorf("%s: too many failed objects, expected at most %d - currently there are: successful=%d failed=%d all=%d",
						options.Summary(), options.MaxFailedObjectCount, successful, failed, all)
				}
				return nil
			}
		}
	}
}

// countObjectsMatchingConditions counts objects that have a successful or failed condition.
// Function assumes the conditions it looks for are mutually exclusive.
func countObjectsMatchingConditions(objectConditions [][]metav1.Condition, successfulConditions []string, failedConditions []string) (successful int, failed int, all int) {
	successfulMap := map[string]bool{}
	for _, c := range successfulConditions {
		successfulMap[c] = true
	}
	failedMap := map[string]bool{}
	for _, c := range failedConditions {
		failedMap[c] = true
	}

	all = len(objectConditions)
	for _, conditions := range objectConditions {
		for _, c := range conditions {
			if successfulMap[conditionToKey(c)] {
				successful++
				break
			}
			if failedMap[conditionToKey(c)] {
				failed++
				break
			}
		}
	}
	return
}

func conditionToKey(c metav1.Condition) string {
	return fmt.Sprintf("%s=%s", c.Type, c.Status)
}
