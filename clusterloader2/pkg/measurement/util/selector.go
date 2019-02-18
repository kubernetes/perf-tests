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

package util

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateSelectorsString creates a string representation for given namespace, label selector and field selector.
func CreateSelectorsString(namespace, labelSelector, fieldSelector string) string {
	var selectorsStrings []string
	if namespace != metav1.NamespaceAll {
		selectorsStrings = append(selectorsStrings, fmt.Sprintf("namespace(%s)", namespace))
	}
	if labelSelector != "" {
		selectorsStrings = append(selectorsStrings, fmt.Sprintf("labelSelector(%s)", labelSelector))
	}
	if fieldSelector != "" {
		selectorsStrings = append(selectorsStrings, fmt.Sprintf("fieldSelector(%s)", fieldSelector))
	}
	if len(selectorsStrings) == 0 {
		return "everything"
	}
	return fmt.Sprint(strings.Join(selectorsStrings, ", "))
}
