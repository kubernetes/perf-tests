/*
Copyright 2017 The Kubernetes Authors.

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

package framework

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

// CreateNSIfNotExists creates a namespace if it is new, otherwise it will return the existing namespace pointer
func CreateNSIfNotExists(f *framework.Framework, namespaceName string) (*v1.Namespace, error) {
	var ns *v1.Namespace
	var err error
	fullNamespace := getNamespace(f, namespaceName)
	if fullNamespace == "" {
		ns, err = f.CreateNamespace(namespaceName, nil)
		if err != nil {
			return nil, err
		}
		framework.Logf("Created new namespace: %s", namespaceName)
	} else {
		ns, err = f.ClientSet.CoreV1().Namespaces().Get(fullNamespace, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		framework.Logf("Namespace exists %s ", namespaceName)
	}
	return ns, err
}

// getNamespace takes the basename from the config and returns the full generated namespace name
func getNamespace(f *framework.Framework, baseName string) string {
	existingNamespaces, _ := f.ClientSet.Core().Namespaces().List(metav1.ListOptions{})
	for _, value := range existingNamespaces.Items {
		if value.GenerateName == fmt.Sprintf("e2e-tests-%v-", baseName) {
			return value.Name
		}
	}
	return ""
}

// ParseConfig unmarshalls the json file defined in the CL config into a struct
func (cl *ClusterLoaderObject) ParseConfig() (*v1.Pod, error) {
	pod := &v1.Pod{}
	// If the file is defined used that as the config
	if cl.File != "" {
		configFile, err := ioutil.ReadFile(MakePath(cl.File))
		if err != nil {
			return pod, err
		}

		if err = json.Unmarshal(configFile, &pod); err != nil {
			return pod, err
		}
	} else if cl.Image != "" && cl.Basename != "" {
		// Otherwise if we have the image name use that instead
		zero := int64(0)
		pod.Spec = v1.PodSpec{TerminationGracePeriodSeconds: &zero, Containers: []v1.Container{
			{
				Name:  cl.Basename,
				Image: cl.Image,
			},
		},
		}
	} else {
		return pod, errors.New("Missing both config file and imagename")
	}

	return pod, nil
}

// MakePath returns fully qualified file location as a string
func MakePath(file string) string {
	// Handle an empty filename.
	if file == "" {
		framework.Failf("No template file defined!")
	}
	// TODO: We should enable passing this as a flag instead of hardcoding.
	return filepath.Join(os.Getenv("GOPATH"), "src/k8s.io/perf-tests/clusterloader/content/", file)
}

// ConvertToLabelSet will convert the string label to a set, while also setting a default value
func (cl *ClusterLoaderObject) ConvertToLabelSet() (labels.Set, error) {
	if cl.Label == "" {
		cl.Label = "purpose=test"
	}
	label, err := labels.ConvertSelectorToLabelsMap(cl.Label)
	return label, err
}
