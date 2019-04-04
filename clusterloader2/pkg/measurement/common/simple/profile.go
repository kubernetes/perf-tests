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

package simple

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	memoryProfileName = "MemoryProfile"
	cpuProfileName    = "CPUProfile"
)

func init() {
	measurement.Register(memoryProfileName, createMemoryProfileMeasurement)
	measurement.Register(cpuProfileName, createCPUProfileMeasurement)
}

type profileConfig struct {
	componentName string
	provider      string
	host          string
	kind          string
}

func createProfileConfig(config *measurement.MeasurementConfig) (*profileConfig, error) {
	var err error
	pc := &profileConfig{}
	if pc.componentName, err = util.GetString(config.Params, "componentName"); err != nil {
		return nil, err
	}
	if pc.provider, err = util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider); err != nil {
		return nil, err
	}
	if pc.host, err = util.GetStringOrDefault(config.Params, "host", config.ClusterFramework.GetClusterConfig().MasterIP); err != nil {
		return nil, err
	}
	return pc, nil

}

type profileMeasurement struct {
	config    *profileConfig
	summaries []measurement.Summary
	isRunning bool
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func (p *profileMeasurement) start(config *measurement.MeasurementConfig, profileKind string) error {
	var err error
	p.config, err = createProfileConfig(config)
	if err != nil {
		return err
	}
	p.config.kind = profileKind
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
				profileSummaries, err := gatherProfile(config, p.config)
				if err != nil {
					klog.Errorf("failed to gather profile for %#v", *p.config)
					continue
				}
				p.summaries = append(p.summaries, profileSummaries...)
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

func createMemoryProfileMeasurement() measurement.Measurement {
	return &memoryProfileMeasurement{}
}

type memoryProfileMeasurement struct {
	measurement profileMeasurement
}

// Execute gathers memory profile of a given component.
func (c *memoryProfileMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if c.measurement.isRunning {
			klog.Infof("%s: measurement already running", c)
			return nil, nil
		}
		return nil, c.measurement.start(config, "heap")
	case "gather":
		c.measurement.stop()
		return c.measurement.summaries, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (*memoryProfileMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*memoryProfileMeasurement) String() string {
	return memoryProfileName
}

func createCPUProfileMeasurement() measurement.Measurement {
	return &cpuProfileMeasurement{}
}

type cpuProfileMeasurement struct {
	measurement profileMeasurement
}

// Execute gathers cpu profile of a given component.
func (c *cpuProfileMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if c.measurement.isRunning {
			klog.Infof("%s: measurement already running", c)
			return nil, nil
		}
		return nil, c.measurement.start(config, "profile")
	case "gather":
		c.measurement.stop()
		return c.measurement.summaries, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (*cpuProfileMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*cpuProfileMeasurement) String() string {
	return cpuProfileName
}

func gatherProfile(caller *measurement.MeasurementConfig, config *profileConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	profilePort, err := getPortForComponent(config.componentName)
	if err != nil {
		return summaries, fmt.Errorf("profile gathering failed finding component port: %v", err)
	}

	// Get the profile data over SSH.
	getCommand := fmt.Sprintf("curl -s localhost:%v/debug/pprof/%s", profilePort, config.kind)
	sshResult, err := measurementutil.SSH(getCommand, config.host+":22", config.provider)
	if err != nil {
		if config.provider == "gke" {
			// Only logging error for gke. SSHing to gke master is not supported.
			klog.Errorf("%s: failed to execute curl command on master through SSH: %v", caller, err)
			return summaries, nil
		}
		return summaries, fmt.Errorf("failed to execute curl command on master through SSH: %v", err)
	}

	profilePrefix := config.componentName
	switch {
	case config.kind == "heap":
		profilePrefix += "_MemoryProfile"
	case strings.HasPrefix(config.kind, "profile"):
		profilePrefix += "_CPUProfile"
	// TODO(wojtekt): Add memory allocations profile.
	default:
		return summaries, fmt.Errorf("unknown profile kind provided: %s", config.kind)
	}

	rawprofile := &profileSummary{
		name:    profilePrefix,
		content: sshResult.Stdout,
	}
	summaries = append(summaries, rawprofile)
	return summaries, nil
}

func getPortForComponent(componentName string) (int, error) {
	switch componentName {
	case "kube-apiserver":
		return 8080, nil
	case "kube-scheduler":
		return 10251, nil
	case "kube-controller-manager":
		return 10252, nil
	}
	return -1, fmt.Errorf("port for component %v unknown", componentName)
}

type profileSummary struct {
	name    string
	content string
}

// SummaryName returns name of the summary.
func (p *profileSummary) SummaryName() string {
	return p.name
}

// PrintSummary returns summary as a string.
func (p *profileSummary) PrintSummary() (string, error) {
	return p.content, nil
}
