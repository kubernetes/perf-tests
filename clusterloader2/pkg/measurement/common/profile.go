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
	"fmt"
	"sync"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	cpuProfileName    = "CPUProfile"
	memoryProfileName = "MemoryProfile"
	mutexProfileName  = "MutexProfile"
)

func init() {
	if err := measurement.Register(cpuProfileName, createProfileMeasurementFactory(cpuProfileName, "profile")); err != nil {
		klog.Fatalf("Cannot register %s: %v", cpuProfileName, err)
	}
	if err := measurement.Register(memoryProfileName, createProfileMeasurementFactory(memoryProfileName, "heap")); err != nil {
		klog.Fatalf("Cannot register %s: %v", memoryProfileName, err)
	}
	if err := measurement.Register(mutexProfileName, createProfileMeasurementFactory(mutexProfileName, "mutex")); err != nil {
		klog.Fatalf("Cannot register %s: %v", mutexProfileName, err)
	}
}

type profileConfig struct {
	componentName string
	provider      string
	host          string
	kind          string
}

func (p *profileMeasurement) populateProfileConfig(config *measurement.MeasurementConfig) error {
	var err error
	if p.config.componentName, err = util.GetString(config.Params, "componentName"); err != nil {
		return err
	}
	if p.config.provider, err = util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider); err != nil {
		return err
	}
	if p.config.host, err = util.GetStringOrDefault(config.Params, "host", config.ClusterFramework.GetClusterConfig().GetMasterIp()); err != nil {
		return err
	}
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

func (p *profileMeasurement) start(config *measurement.MeasurementConfig) error {
	if err := p.populateProfileConfig(config); err != nil {
		return err
	}
	p.summaries = make([]measurement.Summary, 0)
	p.isRunning = true
	p.stopCh = make(chan struct{})
	p.wg.Add(1)

	// Currently length of the test is proportional to the cluster size.
	// So for now we make the profiling frequency proportional to the cluster size.
	// We may want to revisit ot adjust it in the future.
	numNodes := config.ClusterFramework.GetClusterConfig().Nodes
	profileFrequency := time.Duration(5+numNodes/250) * time.Minute

	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-p.stopCh:
				return
			case <-time.After(profileFrequency):
				profileSummary, err := p.gatherProfile(config.ClusterFramework.GetClientSets().GetClient())
				if err != nil {
					klog.Errorf("failed to gather profile for %#v: %v", *p.config, err)
					continue
				}
				if profileSummary != nil {
					p.summaries = append(p.summaries, profileSummary)
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
func (p *profileMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if p.isRunning {
			klog.Infof("%s: measurement already running", p)
			return nil, nil
		}
		return nil, p.start(config)
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

func (p *profileMeasurement) gatherProfile(c clientset.Interface) (measurement.Summary, error) {
	profilePrefix := fmt.Sprintf("%s_%s", p.config.componentName, p.name)
	if p.config.componentName == "kube-apiserver" {
		body, err := c.CoreV1().RESTClient().Get().AbsPath("/debug/pprof/" + p.config.kind).DoRaw()
		if err != nil {
			return nil, err
		}
		return measurement.CreateSummary(profilePrefix, "pprof", string(body)), err
	}

	profilePort, err := getPortForComponent(p.config.componentName)
	if err != nil {
		return nil, fmt.Errorf("profile gathering failed finding component port: %v", err)
	}
	// Get the profile data over SSH.
	getCommand := fmt.Sprintf("curl -s localhost:%v/debug/pprof/%s", profilePort, p.config.kind)
	sshResult, err := measurementutil.SSH(getCommand, p.config.host+":22", p.config.provider)
	if err != nil {
		if p.config.provider == "gke" {
			// Only logging error for gke. SSHing to gke master is not supported.
			klog.Errorf("%s: failed to execute curl command on master through SSH: %v", p.name, err)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to execute curl command on master through SSH: %v", err)
	}

	return measurement.CreateSummary(profilePrefix, "pprof", sshResult.Stdout), err
}

func getPortForComponent(componentName string) (int, error) {
	switch componentName {
	case "etcd":
		return 2382, nil
	case "kube-scheduler":
		return 10251, nil
	case "kube-controller-manager":
		return 10252, nil
	}
	return -1, fmt.Errorf("port for component %v unknown", componentName)
}
