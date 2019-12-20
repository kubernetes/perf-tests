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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/test/e2e/framework"
	kutils "k8s.io/kubernetes/test/utils"
)

// CreateRC will create a new RC if it does not exist, or it will update an existing RC with a new replica count if it does exist
func CreateRC(f *framework.Framework, name, namespace string, label labels.Set, spec v1.PodSpec, replicas int) error {
	_, err := f.ClientSet.Core().ReplicationControllers(namespace).Get(name, metav1.GetOptions{})
	// If RC is not found, Get() will return a NotFound error
	if errors.IsNotFound(err) {
		newRC := newRC(name, int32(replicas), label, spec)
		if _, err := createNewRCWithRetries(f, name, namespace, newRC); err != nil {
			return err
		}
	} else {
		updateReplicas := func(update *v1.ReplicationController) {
			x := int32(replicas)
			update.Spec.Replicas = &x
		}
		if _, err := framework.UpdateReplicationControllerWithRetries(f.ClientSet, namespace, name, updateReplicas); err != nil {
			return err
		}
	}

	// Wait for pods running matching label using podstore
	// does not take replica count into effect, nor owner reference
	return kutils.WaitForPodsWithLabelRunning(f.ClientSet, namespace, labels.SelectorFromSet(label))
}

// createNewRCWithRetries uses polling to retry RC creation
func createNewRCWithRetries(f *framework.Framework, name, namespace string, rcObj *v1.ReplicationController) (rc *v1.ReplicationController, err error) {
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		if rc, err = f.ClientSet.Core().ReplicationControllers(namespace).Create(rcObj); err == nil {
			framework.Logf("Created replication controller %q", name)
			break
		}
	}
	return
}

// newRC creates a new ReplicationController config object
func newRC(rsName string, replicas int32, rcPodLabels map[string]string, spec v1.PodSpec) *v1.ReplicationController {
	return &v1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name: rsName,
		},
		Spec: v1.ReplicationControllerSpec{
			Replicas: func(i int32) *int32 { return &i }(replicas),
			Template: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: rcPodLabels,
				},
				Spec: spec,
			},
		},
	}
}
