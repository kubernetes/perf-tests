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

package utils

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	requestTimeoutSeconds = 1
	highestPortNumber     = 65535
	NameIndex             = "name"
	MaxRetryBackoff       = 5 * time.Minute
)

type BoolMapWithLock struct {
	Mp   map[string]bool
	Lock sync.RWMutex
}

type TimeMapWithLock struct {
	Mp   map[string]time.Time
	Lock sync.RWMutex
}

type TimeWithLock struct {
	Time *time.Time
	Lock sync.RWMutex
}

type TargetSpec struct {
	IP, Name, Namespace string
	Port                int
	StartTime           time.Time
}

type PodEvent struct {
	PodName    string
	IsAddEvent bool
}

// BaseTestClientConfig contains the common configuration required for a test
// client to run.
type BaseTestClientConfig struct {
	HostConfig   *HostConfig
	TargetConfig *TargetConfig

	MainStopChan  chan os.Signal
	MetricsServer *http.Server
	K8sClient     *clientset.Clientset
}

// HostConfig holds information about the pod specification where the
// application runs.
type HostConfig struct {
	// HostNamespace is the namespace of test client pods.
	HostNamespace string
	// MetricsPort is the port number where Prometheus metrics are exposed.
	MetricsPort int
}

// TargetConfig is configuration for target pods.
type TargetConfig struct {
	// TargetLabelSelector is the label selector of target pods to send requests
	// to.
	TargetLabelSelector string
	// TargetNamespace is the namespace of target pods to send requests to.
	TargetNamespace string
	// TargetPort is the port number of target pods to send requests to.
	TargetPort int
	// MaxTargets is the maximum number of target pods to send requests to.
	MaxTargets int
}

// CreateBaseTestClientConfig creates base test client configuration based on
// command-line flags. It parses the flags with "flag.Parse()". All flags need
// to be defined before calling this function.
func CreateBaseTestClientConfig(stopChan chan os.Signal) (*BaseTestClientConfig, error) {
	k8sClient, err := NewK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client, error: %v", err)
	}

	hostConfig := &HostConfig{}
	targetConfig := &TargetConfig{}

	// Parse common command-line flags.
	flag.StringVar(&hostConfig.HostNamespace, "HostNamespace", "", "The namespace of test client pods")
	flag.IntVar(&hostConfig.MetricsPort, "MetricsPort", 9154, "The port number where Prometheus metrics are exposed")
	flag.StringVar(&targetConfig.TargetLabelSelector, "TargetLabelSelector", "", "The label selector of target pods to send requests to")
	flag.StringVar(&targetConfig.TargetNamespace, "TargetNamespace", "", "The namespace of target pods to send requests to")
	flag.IntVar(&targetConfig.TargetPort, "TargetPort", 0, "The port number of target pods to send requests to")
	flag.IntVar(&targetConfig.MaxTargets, "MaxTargets", 100, "The maximum number of target pods to send requests to")
	flag.Parse()

	// Log config values that are parsed from the flags.
	flag.VisitAll(func(f *flag.Flag) {
		klog.Infof("--%s=%s\n", f.Name, f.Value)
	})

	config := &BaseTestClientConfig{
		HostConfig:   hostConfig,
		TargetConfig: targetConfig,
		MainStopChan: stopChan,
		K8sClient:    k8sClient,
	}

	return config, nil
}

// VerifyHostConfig ensures that host configuration is valid.
func VerifyHostConfig(hostConfig *HostConfig) error {
	if len(hostConfig.HostNamespace) == 0 {
		return fmt.Errorf("HostNamespace parameter is not specified")
	}
	if hostConfig.MetricsPort < 1 || hostConfig.MetricsPort > highestPortNumber {
		return fmt.Errorf("MetricsPort is out of range [1..%d]", highestPortNumber)
	}

	return nil
}

// VerifyTargetConfig ensures that target configuration is valid.
func VerifyTargetConfig(targetConfig *TargetConfig) error {
	if len(targetConfig.TargetLabelSelector) == 0 {
		return fmt.Errorf("TargetLabelSelector parameter is not specified")
	}

	if len(targetConfig.TargetNamespace) == 0 {
		return fmt.Errorf("TargetNamespace parameter is not specified")
	}

	if targetConfig.TargetPort < 1 || targetConfig.TargetPort > highestPortNumber {
		return fmt.Errorf("TargetPort parameter is out of range [1..%d]", highestPortNumber)
	}

	if targetConfig.MaxTargets < 1 {
		return fmt.Errorf("MaxTargets parameter is invalid: %d", targetConfig.MaxTargets)
	}

	return nil
}

// ShutDownMetricsServer stops the specified metrics server.
func ShutDownMetricsServer(ctx context.Context, metricsServer *http.Server) {
	klog.Info("Shutting down metrics server")
	if err := metricsServer.Shutdown(ctx); err != nil {
		klog.Errorf("Metrics server shutdown failed, error: %v", err)
	}
}

// RecordFirstSuccessfulRequest sends curl requests continuously to the provided
// target IP and target port from the test client config, at 1 second intervals,
// until the first successful response. It records the latency between the
// provided start time in the targetSpec, and the time of the first successful
// response.
func RecordFirstSuccessfulRequest(target *TargetSpec, stopChan chan os.Signal, successFunc func(*TargetSpec, time.Time)) {
	if target == nil || len(target.IP) == 0 {
		klog.Warningf("Skipping a target for policy enforcement latency. Target needs to have IP address and port specified. Target: %v", target)
		return
	}

	request := fmt.Sprintf("curl --connect-timeout %d %s:%d", requestTimeoutSeconds, target.IP, target.Port)
	command := []string{"sh", "-c", "-x", request}
	klog.Infof("Sending requests %q", request)

	for {
		select {
		case <-stopChan:
			return
		default:
		}

		_, err := runCommand(command)
		// CURL command returns errors for requests denied by a policy, or when the
		// IP is unreachable. Everything else is considered a success.
		if err != nil {
			continue
		}

		successFunc(target, time.Now())
		return
	}
}

// NewK8sClient returns a K8s client for the K8s cluster where this application
// runs inside a pod.
func NewK8sClient() (*clientset.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return clientset.NewForConfig(config)
}

// Retry runs a function until it succeeds, with specified number of attempts
// and base for exponential backoff.
func Retry(attempts int, backoff time.Duration, fn func() error) error {
	if err := fn(); err != nil {
		newBackoff := backoff * 2
		if attempts > 1 && newBackoff < MaxRetryBackoff {
			time.Sleep(backoff)
			return Retry(attempts-1, newBackoff, fn)
		}
		return err
	}

	return nil
}

// InformerSynced verifies that the provided sync function is successful.
func InformerSynced(syncFunc func() bool, informerName string) error {
	yes := syncFunc()
	if yes {
		return nil
	}

	return fmt.Errorf("failed to sync informer %s", informerName)
}

// MetaNameIndexFunc is an index function that indexes based on object's name.
func MetaNameIndexFunc(obj interface{}) ([]string, error) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err)
	}
	return []string{m.GetName()}, nil
}

// RunCommand executes a command based on the provided string slice. The command
// is constructed by taking the first element as the command name, and then all
// the subsequent elements as arguments to that command.
func runCommand(cmd []string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		return stderr.String(), err
	}
	return stdout.String(), nil
}

// ChannelIsOpen checks if the specified channel is open.
func ChannelIsOpen(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return false
	default:
	}

	return true
}

// EnterIdleState is used for preventing the pod from exiting after the test is
// completed, because it then gets restarted. The test client enters the idle
// state as soon as it has finished the setup and has run the test goroutines.
func EnterIdleState(stopChan chan os.Signal) {
	klog.Info("Going into idle state")
	select {
	case <-stopChan:
		klog.Info("Exiting idle state")
		return
	}
}
