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

package common

import (
	"context"
	"fmt"
	"sync"
	"time"

	goerrors "github.com/go-errors/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	cpuProfileName    = "CPUProfile"
	memoryProfileName = "MemoryProfile"
	blockProfileName  = "BlockProfile"
)

func init() {
	if err := measurement.Register(cpuProfileName, createProfileMeasurementFactory(cpuProfileName, "profile")); err != nil {
		klog.Fatalf("Cannot register %s: %v", cpuProfileName, err)
	}
	if err := measurement.Register(memoryProfileName, createProfileMeasurementFactory(memoryProfileName, "heap")); err != nil {
		klog.Fatalf("Cannot register %s: %v", memoryProfileName, err)
	}
	if err := measurement.Register(blockProfileName, createProfileMeasurementFactory(blockProfileName, "block")); err != nil {
		klog.Fatalf("Cannot register %s: %v", blockProfileName, err)
	}
}

type profileConfig struct {
	componentName string
	provider      provider.Provider
	hosts         []string
	kind          string
}

func (p *profileMeasurement) populateProfileConfig(config *measurement.Config) error {
	var err error
	if p.config.componentName, err = util.GetString(config.Params, "componentName"); err != nil {
		return err
	}
	p.config.provider = config.ClusterFramework.GetClusterConfig().Provider
	p.config.hosts = config.ClusterFramework.GetClusterConfig().MasterIPs
	return nil
}

type profileMeasurement struct {
	name      string
	config    *profileConfig
	summaries []measurement.Summary
	isRunning bool
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func createProfileMeasurementFactory(name, kind string) func() measurement.Measurement {
	return func() measurement.Measurement {
		return &profileMeasurement{
			name:   name,
			config: &profileConfig{kind: kind},
		}
	}
}

func (p *profileMeasurement) start(config *measurement.Config, SSHToMasterSupported bool) error {
	if err := p.populateProfileConfig(config); err != nil {
		return err
	}
	if len(p.config.hosts) < 1 {
		klog.Warning("Profile measurements will be disabled due to no MasterIps")
		return nil
	}
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()
	if p.shouldExposeAPIServerDebugEndpoint() {
		if err := exposeAPIServerDebugEndpoint(k8sClient); err != nil {
			klog.Warningf("error while exposing kube-apiserver /debug endpoint: %v", err)
		}
	}

	p.summaries = make([]measurement.Summary, 0)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	p.wg.Add(1)

	profileFrequency := 5 * time.Minute
	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-p.stopCh:
				return
			case <-time.After(profileFrequency):
				profileSummaries, err := p.gatherProfile(k8sClient, SSHToMasterSupported, config)
				if err != nil {
					klog.Errorf("failed to gather profile for %#v: %v", *p.config, err)
					continue
				}
				if profileSummaries != nil {
					p.summaries = append(p.summaries, profileSummaries...)
				}
			}
		}
	}()
	return nil
}

func (p *profileMeasurement) stop() {
	if !p.isRunning {
		return
	}
	close(p.stopCh)
	p.wg.Wait()
}

// Execute gathers memory profile of a given component.
func (p *profileMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	provider := config.ClusterFramework.GetClusterConfig().Provider
	SSHToMasterSupported := provider.Features().SupportSSHToMaster
	APIServerPprofEnabled := config.ClusterFramework.GetClusterConfig().APIServerPprofByClientEnabled

	if !SSHToMasterSupported && APIServerPprofEnabled {
		klog.Warningf("fetching profile data from is not possible from provider: %s", provider.Name())
		return nil, nil
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if p.isRunning {
			klog.V(2).Infof("%s: measurement already running", p)
			return nil, nil
		}
		return nil, p.start(config, SSHToMasterSupported)
	case "gather":
		p.stop()
		return p.summaries, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (*profileMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (p *profileMeasurement) String() string {
	return p.name
}

func (p *profileMeasurement) gatherProfile(c clientset.Interface, SSHToMasterSupported bool, config *measurement.Config) ([]measurement.Summary, error) {
	getCommand, err := p.getProfileCommand(config)
	if err != nil {
		return nil, goerrors.Errorf("profile gathering failed during retrieving profile command: %v", err)
	}

	var summaries []measurement.Summary
	for _, host := range p.config.hosts {
		profilePrefix := fmt.Sprintf("%s_%s_%s", host, p.config.componentName, p.name)

		// Get the profile data over SSH.
		// Start by checking that the provider allows us to do so.
		if !SSHToMasterSupported {
			// SSH to master for this provider is not possible.
			// For kube-apiserver, we can still fetch the profile using a RESTClient and pprof.
			// TODO(#246): This will connect to a random master in HA (multi-master) clusters, fix it.
			if p.config.componentName == "kube-apiserver" {
				body, err := c.CoreV1().RESTClient().Get().AbsPath("/debug/pprof/" + p.config.kind).DoRaw(context.TODO())
				if err != nil {
					return nil, err
				}
				summary := measurement.CreateSummary(profilePrefix, "pprof", string(body))
				summaries = append(summaries, summary)
				break
			}
			// Only logging error for gke. SSHing to gke master is not supported.
			klog.Warningf("%s: failed to execute curl command on master through SSH", p.name)
			return nil, nil
		}

		sshResult, err := measurementutil.SSH(getCommand, host+":22", p.config.provider)
		if err != nil {
			return nil, fmt.Errorf("failed to execute curl command on master node %s through SSH: %v", host, err)
		}
		summaries = append(summaries, measurement.CreateSummary(profilePrefix, "pprof", sshResult.Stdout))
	}

	return summaries, nil
}

func (p *profileMeasurement) shouldExposeAPIServerDebugEndpoint() bool {
	return p.config.componentName == "kube-apiserver"
}

func (p *profileMeasurement) getProfileCommand(config *measurement.Config) (string, error) {
	profileProtocol, profilePort, err := config.ClusterFramework.GetClusterConfig().Provider.GetComponentProtocolAndPort(p.config.componentName)
	if err != nil {
		return "", goerrors.Errorf("get profile command failed finding component protocol/port: %v", err)
	}

	var command string
	if p.config.componentName == "etcd" {
		etcdCert := config.ClusterFramework.GetClusterConfig().EtcdCertificatePath
		etcdKey := config.ClusterFramework.GetClusterConfig().EtcdKeyPath
		command = fmt.Sprintf("curl -s -k --cert %s --key %s %slocalhost:%v/debug/pprof/%s", etcdCert, etcdKey, profileProtocol, profilePort, p.config.kind)
	} else {
		command = fmt.Sprintf("curl -s -k %slocalhost:%v/debug/pprof/%s", profileProtocol, profilePort, p.config.kind)
	}

	return command, nil
}

func exposeAPIServerDebugEndpoint(c clientset.Interface) error {
	klog.V(2).Info("Exposing kube-apiserver debug endpoint for anonymous access")
	createClusterRole := func() error {
		_, err := c.RbacV1().ClusterRoles().Create(context.TODO(), &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "apiserver-debug-viewer"},
			Rules: []rbacv1.PolicyRule{
				{Verbs: []string{"get"}, NonResourceURLs: []string{"/debug/*"}},
			},
		}, metav1.CreateOptions{})
		return err
	}
	createClusterRoleBinding := func() error {
		_, err := c.RbacV1().ClusterRoleBindings().Create(context.TODO(), &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "anonymous:apiserver-debug-viewer"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "apiserver-debug-viewer"},
			Subjects: []rbacv1.Subject{
				{Kind: "User", Name: "system:anonymous"},
			},
		}, metav1.CreateOptions{})
		return err
	}
	if err := retryCreateFunction(createClusterRole); err != nil {
		return err
	}
	if err := retryCreateFunction(createClusterRoleBinding); err != nil {
		return err
	}
	return nil
}

func retryCreateFunction(f func() error) error {
	return client.RetryWithExponentialBackOff(
		client.RetryFunction(f, client.Allow(apierrs.IsAlreadyExists)))
}
