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
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	sshutil "k8s.io/kubernetes/pkg/ssh"
)

// GetMasterHost turns host name (without prefix and port).
func GetMasterHost(host string) (string, error) {
	masterUrl, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	return masterUrl.Hostname(), nil
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
func SSH(cmd, host, provider string) (SSHResult, error) {
	result := SSHResult{Host: host, Cmd: cmd}

	// Get a signer for the provider.
	signer, err := getSigner(provider)
	if err != nil {
		return result, fmt.Errorf("error getting signer for provider %s: '%v'", provider, err)
	}

	// RunSSHCommand will default to Getenv("USER") if user == "", but we're
	// defaulting here as well for logging clarity.
	result.User = os.Getenv("KUBE_SSH_USER")
	if result.User == "" {
		result.User = os.Getenv("USER")
	}

	stdout, stderr, code, err := sshutil.RunSSHCommand(cmd, result.User, host, signer)
	result.Stdout = stdout
	result.Stderr = stderr
	result.Code = code

	return result, err
}

// getSigner returns an ssh.Signer for the provider ("gce", etc.) that can be
// used to SSH to their nodes.
func getSigner(provider string) (ssh.Signer, error) {
	// honor a consistent SSH key across all providers
	if path := os.Getenv("KUBE_SSH_KEY_PATH"); len(path) > 0 {
		return sshutil.MakePrivateKeySignerFromFile(path)
	}

	// Select the key itself to use. When implementing more providers here,
	// please also add them to any SSH tests that are disabled because of signer
	// support.
	keyfile := ""
	switch provider {
	case "gce", "gke":
		keyfile = os.Getenv("GCE_SSH_KEY")
		if keyfile == "" {
			keyfile = "google_compute_engine"
		}
	case "kubemark":
		keyfile = os.Getenv("KUBEMARK_SSH_KEY")
		if keyfile == "" {
			keyfile = "google_compute_engine"
		}
	case "aws", "eks":
		keyfile := os.Getenv("AWS_SSH_KEY")
		if keyfile == "" {
			keyfile = "kube_aws_rsa"
		}
	case "local", "vsphere":
		keyfile = os.Getenv("LOCAL_SSH_KEY")
		if keyfile == "" {
			keyfile = "id_rsa"
		}
	case "skeleton":
		keyfile = os.Getenv("KUBE_SSH_KEY")
		if keyfile == "" {
			keyfile = "id_rsa"
		}
	default:
		return nil, fmt.Errorf("GetSigner(...) not implemented for %s", provider)
	}

	// Respect absolute paths for keys given by user, fallback to assuming
	// relative paths are in ~/.ssh
	if !filepath.IsAbs(keyfile) {
		keydir := filepath.Join(os.Getenv("HOME"), ".ssh")
		keyfile = filepath.Join(keydir, keyfile)
	}

	return sshutil.MakePrivateKeySignerFromFile(keyfile)
}
