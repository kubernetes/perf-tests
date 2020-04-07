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

package common

import (
	"context"
	"fmt"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"sync"
	"time"

	goerrors "github.com/go-errors/errors"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	apiAvailabilityName = "APIAvailability"
)

func init() {
	if err := measurement.Register(apiAvailabilityName, createAPIAvailabilityMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", apiAvailabilityName, err)
	}
}

func createAPIAvailabilityMeasurement() measurement.Measurement {
	return &apiAvailabilityMeasurement{}
}

type apiAvailabilityMeasurement struct {
	isRunning           bool
	stopCh              chan struct{}
	hosts               []string
	summaries           []measurement.Summary
	clusterLevelMetrics *apiAvailabilityMetrics
	hostLevelMetrics    map[string]*apiAvailabilityMetrics
	wg                  sync.WaitGroup
}

func (a *apiAvailabilityMeasurement) updateMasterAvailabilityMetrics(c clientset.Interface, config *measurement.Config, provider provider.Provider) {
	for _, host := range a.hosts {
		// SSH and check the health of the host
		command := fmt.Sprintf("curl -f -s -k %slocalhost:%v/healthz", "https://", 443)
		sshResult, err := measurementutil.SSH(command, host+":22", provider)
		availability := err != nil || sshResult.Code != 0
		a.updateAvailabilityMetrics(availability, a.hostLevelMetrics[host])
	}
}

func (a *apiAvailabilityMeasurement) updateClusterAvailabilityMetrics(c clientset.Interface) {
	// Check the availability of the cluster by issuing a REST call to /healthz end point
	result := c.CoreV1().RESTClient().Get().AbsPath("/healthz").Do(context.TODO())
	status := 0
	result.StatusCode(&status)
	a.updateAvailabilityMetrics(status == 200, a.clusterLevelMetrics)
}

func (a *apiAvailabilityMeasurement) start(config *measurement.Config, SSHToMasterSupported bool, probeDuration int) error {
	a.hosts = config.ClusterFramework.GetClusterConfig().MasterIPs
	if len(a.hosts) < 1 {
		return goerrors.Errorf("APIAvailability measurement can't start due to lack of master IPs")
	}

	k8sClient := config.ClusterFramework.GetClientSets().GetClient()
	provider := config.ClusterFramework.GetClusterConfig().Provider

	a.isRunning = true
	a.stopCh = make(chan struct{})
	a.wg.Add(1)

	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-a.stopCh:
				return
			case <-time.After(time.Duration(probeDuration)):
				a.updateClusterAvailabilityMetrics(k8sClient)
				if SSHToMasterSupported {
					a.updateMasterAvailabilityMetrics(k8sClient, config, provider)
				}
			}
		}
	}()
	return nil
}

// Execute starts the api-server healthz probe end point from start action and
// collects availability metrics in gather.
func (a *apiAvailabilityMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	SSHToMasterSupported := !config.ClusterFramework.GetClusterConfig().Provider.Features().SupportSSHToMaster

	if !SSHToMasterSupported {
		klog.Infof("ssh to master nodes not supported. Measurement would have only Cluster Level Metrics")
	}

	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	probeFrequency, err := util.GetIntOrDefault(config.Params, "frequency", 1)
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if a.isRunning {
			klog.Infof("%s: measurement already running", a)
			return nil, nil
		}
		return nil, a.start(config, SSHToMasterSupported, probeFrequency)
	case "gather":
		err := a.stopAndSummarize(SSHToMasterSupported, probeFrequency)
		if err != nil {
			return nil, err
		}
		return a.summaries, nil
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (a *apiAvailabilityMeasurement) stopAndSummarize(SSHToMasterSupported bool, probeFrequency int) error {
	if !a.isRunning {
		return nil
	}
	close(a.stopCh)
	a.wg.Wait()

	output := apiAvailabilityOutput{}
	output.createClusterSummary(a.clusterLevelMetrics, probeFrequency)
	if SSHToMasterSupported {
		output.createMastersSummary(a.hostLevelMetrics, a.hosts, probeFrequency)
	}

	errList := errors.NewErrorList()
	content, err := util.PrettyPrintJSON(output)
	if err != nil {
		errList.Append(errList, err)
	} else {
		summary := measurement.CreateSummary(apiAvailabilityName, "json", content)
		a.summaries = append(a.summaries, summary)
	}
	return errList
}

// Dispose cleans up after the measurement.
func (a apiAvailabilityMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (a apiAvailabilityMeasurement) String() string {
	return apiAvailabilityName
}
