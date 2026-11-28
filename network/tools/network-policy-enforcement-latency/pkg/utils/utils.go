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
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	requestTimeoutSeconds      = 1
	highestPortNumber          = 65535
	NameIndex                  = "name"
	MaxRetryBackoff            = 5 * time.Minute
	defaultInformerSyncTimeout = 2 * time.Minute
	protobufMediaType          = "application/vnd.kubernetes.protobuf"
	protobufAcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
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

	MainStopChan        chan os.Signal
	StopCh              chan struct{}
	InformerSyncTimeout time.Duration
	ProbeFunc           func(target *TargetSpec) error
	MetricsServer       *http.Server
	K8sClient           *clientset.Clientset

	initStopOnce sync.Once
	stopOnce     sync.Once
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
	config.InitStopChannel()

	return config, nil
}

// InitStopChannel sets up StopCh so a single OS signal on MainStopChan closes
// StopCh and wakes all active informers, probe goroutines, and WaitUntilStopped.
func (c *BaseTestClientConfig) InitStopChannel() {
	c.initStopOnce.Do(func() {
		if c.StopCh == nil {
			c.StopCh = make(chan struct{})
		}
		if c.InformerSyncTimeout <= 0 {
			c.InformerSyncTimeout = defaultInformerSyncTimeout
		}
		if c.MainStopChan != nil {
			go func() {
				select {
				case <-c.MainStopChan:
					c.Stop()
				case <-c.StopCh:
				}
			}()
		}
	})
}

// Stop closes StopCh once.
func (c *BaseTestClientConfig) Stop() {
	c.InitStopChannel()
	c.stopOnce.Do(func() {
		close(c.StopCh)
	})
}

// WaitUntilStopped blocks until StopCh is closed so the test client pod stays
// running for Prometheus scraping without restarting until pod deletion.
func (c *BaseTestClientConfig) WaitUntilStopped() {
	c.InitStopChannel()
	klog.Info("Going into idle state")
	<-c.StopCh
	klog.Info("Exiting idle state")
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

// NewCurlProbe returns a probe that sends one curl request to target. curl runs
// without a shell so that bracketed IPv6 addresses are not expanded as globs.
func NewCurlProbe(target *TargetSpec) func(*TargetSpec) error {
	command := curlCommand(target.IP, target.Port)
	klog.Infof("Sending requests %q", strings.Join(command, " "))
	return func(*TargetSpec) error {
		_, err := runCommand(command)
		return err
	}
}

func curlCommand(ip string, port int) []string {
	return []string{"curl", "-g", "--connect-timeout", strconv.Itoa(requestTimeoutSeconds), net.JoinHostPort(ip, strconv.Itoa(port))}
}

// RecordFirstSuccessfulRequest sends curl requests continuously to the provided
// target IP and target port from the test client config, until the first
// successful response. It records the latency between the provided start time
// in the targetSpec, and the time of the first successful response.
func RecordFirstSuccessfulRequest(target *TargetSpec, stopChan chan os.Signal, successFunc func(*TargetSpec, time.Time)) {
	if target == nil || len(target.IP) == 0 {
		klog.Warningf("Skipping a target for policy enforcement latency. Target needs to have IP address and port specified. Target: %v", target)
		return
	}

	probe := NewCurlProbe(target)
	for {
		select {
		case <-stopChan:
			return
		default:
		}

		// CURL command returns errors for requests denied by a policy, or when the
		// IP is unreachable. Everything else is considered a success.
		if err := probe(target); err != nil {
			continue
		}

		successFunc(target, time.Now())
		return
	}
}

// RecordFirstSuccessfulRequestUntilStop probes target until the first success
// or until stopCh is closed. When probeFunc is nil, it uses the default 1s-timeout curl probe.
func RecordFirstSuccessfulRequestUntilStop(target *TargetSpec, stopCh <-chan struct{}, probeFunc func(*TargetSpec) error, successFunc func(*TargetSpec, time.Time)) {
	if target == nil || len(target.IP) == 0 {
		klog.Warningf("Skipping a target for policy enforcement latency. Target needs to have IP address and port specified. Target: %v", target)
		return
	}

	if probeFunc == nil {
		probeFunc = NewCurlProbe(target)
	}

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		if err := probeFunc(target); err != nil {
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
	return NewK8sClientFromConfig(config)
}

// NewK8sClientFromConfig builds a Clientset configured to negotiate Protobuf
// with JSON fallback for built-in Kubernetes resources.
func NewK8sClientFromConfig(config *rest.Config) (*clientset.Clientset, error) {
	cfg := rest.CopyConfig(config)
	cfg.AcceptContentTypes = protobufAcceptContentTypes
	cfg.ContentType = protobufMediaType
	return clientset.NewForConfig(cfg)
}

// WaitForInformerSync blocks until all informers sync, stopCh closes, or timeout elapses.
func WaitForInformerSync(stopCh <-chan struct{}, timeout time.Duration, cacheSyncs ...cache.InformerSynced) bool {
	ctx, cancel := context.WithTimeout(wait.ContextForChannel(stopCh), timeout)
	defer cancel()
	return cache.WaitForCacheSync(ctx.Done(), cacheSyncs...)
}

// TrimPod retains only the ObjectMeta identity/timestamp fields and PodIP used
// by the latency informers before inserting Pod objects into the cache.
func TrimPod(obj interface{}) (interface{}, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			UID:               pod.UID,
			ResourceVersion:   pod.ResourceVersion,
			CreationTimestamp: pod.CreationTimestamp,
		},
		Status: corev1.PodStatus{
			PodIP: pod.Status.PodIP,
		},
	}, nil
}

// TrimNetworkPolicy retains only the ObjectMeta identity/timestamp fields used
// by the latency informer before inserting NetworkPolicy objects into the cache.
func TrimNetworkPolicy(obj interface{}) (interface{}, error) {
	np, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		return obj, nil
	}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:              np.Name,
			Namespace:         np.Namespace,
			UID:               np.UID,
			ResourceVersion:   np.ResourceVersion,
			CreationTimestamp: np.CreationTimestamp,
		},
	}, nil
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
