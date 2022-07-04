/*
Copyright 2020 The Kubernetes Authors.

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

package provider

import (
	"errors"
	"fmt"

	sshutil "k8s.io/kubernetes/test/e2e/framework/ssh"
)

var (
	ErrNoManagedPrometheus = errors.New("no managed Prometheus service for this cloud provider")
)

func getComponentProtocolAndPort(componentName string) (string, int, error) {
	switch componentName {
	case "etcd":
		return "https://", 2379, nil
	case "kube-apiserver":
		return "https://", 443, nil
	case "kube-controller-manager":
		return "https://", 10257, nil
	case "kube-scheduler":
		return "https://", 10259, nil
	}
	return "", -1, fmt.Errorf("port for component %v unknown", componentName)
}

func runSSHCommand(cmd, host string) (string, string, int, error) {
	// skeleton provider takes ssh key from KUBE_SSH_KEY_PATH and KUBE_SSH_KEY.
	r, err := sshutil.SSH(cmd, host, "skeleton")
	return r.Stdout, r.Stderr, r.Code, err
}
