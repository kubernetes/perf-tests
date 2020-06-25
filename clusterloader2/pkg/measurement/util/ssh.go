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
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"net/url"
)

// GetMasterHost turns host name (without prefix and port).
func GetMasterHost(host string) (string, error) {
	masterURL, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	return masterURL.Hostname(), nil
}

// SSHResult represents result of ssh command.
type SSHResult struct {
	User   string
	Host   string
	Cmd    string
	Stdout string
	Stderr string
	Code   int
}

// SSH runs command on given host using ssh.
func SSH(cmd, host string, provider provider.Provider) (SSHResult, error) {
	result := SSHResult{Host: host, Cmd: cmd}
	stdout, stderr, code, err := provider.RunSSHCommand(cmd, host)
	result.Stdout = stdout
	result.Stderr = stderr
	result.Code = code

	return result, err
}
