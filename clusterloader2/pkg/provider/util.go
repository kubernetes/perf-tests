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
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	sshutil "k8s.io/kubernetes/pkg/ssh"
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
	signer, err := defaultSSHSigner()
	if err != nil {
		return "", "", 0, err
	}
	if signer == nil {
		return "", "", 0, fmt.Errorf("cannot find sshkey")
	}

	user := defaultSSHUser()
	return sshutil.RunSSHCommand(cmd, user, host, signer)
}

func defaultSSHUser() string {
	// RunSSHCommand will default to Getenv("USER") if user == "", but we're
	// defaulting here as well for logging clarity.
	user := os.Getenv("KUBE_SSH_USER")
	if user == "" {
		user = os.Getenv("USER")
	}
	return user
}

func defaultSSHSigner() (ssh.Signer, error) {
	if path := os.Getenv("KUBE_SSH_KEY_PATH"); len(path) > 0 {
		return sshutil.MakePrivateKeySignerFromFile(path)
	}
	return nil, nil
}

func sshSignerFromKeyFile(KeyPathEnv string, defaultKeyPath string) (ssh.Signer, error) {
	// honor a consistent SSH key across all providers
	signer, err := defaultSSHSigner()
	if signer != nil || err != nil {
		return signer, err
	}

	keyfile := os.Getenv(KeyPathEnv)
	if keyfile == "" {
		keyfile = defaultKeyPath
	}

	// Respect absolute paths for keys given by user, fallback to assuming
	// relative paths are in ~/.ssh
	if !filepath.IsAbs(keyfile) {
		keydir := filepath.Join(os.Getenv("HOME"), ".ssh")
		keyfile = filepath.Join(keydir, keyfile)
	}

	if keyfile == "" {
		return nil, fmt.Errorf("cannot find ssh key file")
	}

	return sshutil.MakePrivateKeySignerFromFile(keyfile)
}
