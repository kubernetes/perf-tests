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

package util

import (
	"fmt"
	"io"
	"os/exec"

	"k8s.io/api/core/v1"
	"k8s.io/klog"
)

// SSHExecutor interface can run commands in cluster nodes via SSH
type SSHExecutor interface {
	Exec(command string, node *v1.Node, stdin io.Reader) error
}

// GCloudSSHExecutor runs commands in GCloud cluster nodes
type GCloudSSHExecutor struct{}

// Exec executes command on a given node with stdin provided.
// If stdin is nil, the process reads from null device.
func (e *GCloudSSHExecutor) Exec(command string, node *v1.Node, stdin io.Reader) error {
	zone, ok := node.Labels["topology.kubernetes.io/zone"]
	if !ok {
		// Fallback to old label to make it work for old k8s versions.
		zone, ok = node.Labels["failure-domain.beta.kubernetes.io/zone"]
		if !ok {
			return fmt.Errorf("unknown zone for %q node: no topology-related labels", node.Name)
		}
	}
	cmd := exec.Command("gcloud", "compute", "ssh", "--zone", zone, "--command", command, node.Name)
	cmd.Stdin = stdin
	output, err := cmd.CombinedOutput()
	klog.V(2).Infof("ssh to %q finished with %q: %v", node.Name, string(output), err)
	return err
}
