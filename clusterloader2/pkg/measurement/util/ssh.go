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
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

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

func execLocal(cmd string) (SSHResult, error) {
	result := SSHResult{Cmd: cmd}
	result.User = "localhost"

	c := exec.Command("sh", "-c", cmd)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	if err := c.Start(); err != nil {
		return result, fmt.Errorf("command start error %s: '%v'", cmd, err)
	}
	var code int
	if err := c.Wait(); err != nil {
		exiterr, ok := err.(*exec.ExitError)
		if !ok{
			return result, fmt.Errorf("command wait error %s: '%v'", cmd, err)
		}
		// The command has exited with an exit code != 0
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			code = status.ExitStatus()
		}
	}
	result.Stdout = outBuf.String()
	result.Stderr = errBuf.String()
	result.Code = code

	return result, nil
}

// SSH runs command on given host using ssh.
func SSH(cmd, host, provider string) (SSHResult, error) {
	if host == "localhost:22" {
		return execLocal(cmd)
	}

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
	// Get the directory in which SSH keys are located.
	keydir := filepath.Join(os.Getenv("HOME"), ".ssh")

	// Select the key itself to use. When implementing more providers here,
	// please also add them to any SSH tests that are disabled because of signer
	// support.
	keyfile := ""
	key := ""
	switch provider {
	case "gce", "gke", "kubemark":
		keyfile = "google_compute_engine"
	case "aws":
		// If there is an env. variable override, use that.
		awsKeyfile := os.Getenv("AWS_SSH_KEY")
		if len(awsKeyfile) != 0 {
			return sshutil.MakePrivateKeySignerFromFile(awsKeyfile)
		}
		// Otherwise revert to home dir
		keyfile = "kube_aws_rsa"
	case "local", "vsphere":
		keyfile = os.Getenv("LOCAL_SSH_KEY") // maybe?
		if len(keyfile) == 0 {
			keyfile = "id_rsa"
		}
	case "skeleton":
		keyfile = os.Getenv("KUBE_SSH_KEY")
		if len(keyfile) == 0 {
			keyfile = "id_rsa"
		}
	default:
		return nil, fmt.Errorf("GetSigner(...) not implemented for %s", provider)
	}

	if len(key) == 0 {
		key = filepath.Join(keydir, keyfile)
	}

	return sshutil.MakePrivateKeySignerFromFile(key)
}
