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
	"net/http"
	"strconv"
	"sync"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/execservice"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	apiAvailabilityMeasurementName = "APIAvailability"
)

func init() {
	if err := measurement.Register(apiAvailabilityMeasurementName, createAPIAvailabilityMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", apiAvailabilityMeasurementName, err)
	}
}

func createAPIAvailabilityMeasurement() measurement.Measurement {
	return &apiAvailabilityMeasurement{}
}

type apiAvailabilityMeasurement struct {
	isRunning           bool
	isPaused            bool
	pauseCh             chan struct{}
	unpauseCh           chan struct{}
	stopCh              chan struct{}
	pollFrequency       time.Duration
	hostIPs             []string
	summaries           []measurement.Summary
	clusterLevelMetrics *apiAvailabilityMetrics
	threshold           float64
	// Should we check public IPs of the host VMs
	useHostPublicIPs bool
	// Should we check internal IPs of the host VMs
	useHostInternalIPs bool
	// Metrics per host internal IP.
	hostLevelMetrics           map[string]*apiAvailabilityMetrics
	hostPollTimeoutSeconds     int
	hostPollExecTimeoutSeconds int
	wg                         sync.WaitGroup
	lock                       sync.Mutex
}

func (a *apiAvailabilityMeasurement) updateHostAvailabilityMetrics() {
	wg := sync.WaitGroup{}
	wg.Add(len(a.hostIPs))
	mu := sync.Mutex{}
	for _, ip := range a.hostIPs {
		ip := ip
		go func() {
			defer wg.Done()
			statusCode, err := a.pollHost(ip)
			availability := statusCode == strconv.Itoa(http.StatusOK)
			if err != nil {
				klog.Warningf("execservice issue: %s", err.Error())
			}
			if !availability {
				klog.Warningf("host %s not available; HTTP status code: %s", ip, statusCode)
			}
			mu.Lock()
			defer mu.Unlock()
			a.hostLevelMetrics[ip].update(availability)
		}()
	}
	wg.Wait()
}

func (a *apiAvailabilityMeasurement) pollHost(hostIP string) (string, error) {
	pod, err := execservice.GetPod()
	if err != nil {
		return "", fmt.Errorf("problem with GetPod(): %w", err)
	}
	cmd := fmt.Sprintf("curl --connect-timeout %d -s -k -w \"%%{http_code}\" -o /dev/null https://%s:443/readyz", a.hostPollTimeoutSeconds, hostIP)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.hostPollExecTimeoutSeconds)*time.Second)
	defer cancel()
	output, err := execservice.RunCommand(ctx, pod, cmd)
	if err != nil {
		return "", fmt.Errorf("problem with RunCommand(): output=%q, err=%w", output, err)
	}
	return output, nil
}

func (a *apiAvailabilityMeasurement) updateClusterAvailabilityMetrics(c clientset.Interface) {
	result := c.CoreV1().RESTClient().Get().AbsPath("/readyz").Do(context.Background())
	if err := result.Error(); err != nil {
		klog.Warningf("failed to reach cluster API server: %v", err)
	}
	status := 0
	result.StatusCode(&status)
	availability := status == http.StatusOK
	if !availability {
		klog.Warningf("cluster not available; HTTP status code: %d", status)
	}
	a.clusterLevelMetrics.update(availability)
}

func (a *apiAvailabilityMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	switch action {
	case "start":
		return nil, a.start(config)
	case "pause":
		a.pause()
		return nil, nil
	case "unpause":
		a.unpause()
		return nil, nil
	case "gather":
		return a.gather()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (a *apiAvailabilityMeasurement) start(config *measurement.Config) error {
	if a.isRunning {
		klog.V(2).Infof("%s: measurement already running", a)
		return nil
	}
	if err := a.initFields(config); err != nil {
		return err
	}
	k8sClient := config.ClusterFramework.GetClientSets().GetClient()
	a.wg.Add(1)

	go func() {
		defer a.wg.Done()
		for {
			if a.isPaused {
				select {
				case <-a.unpauseCh:
					a.isPaused = false
				case <-a.stopCh:
					return
				}
			}
			select {
			case <-a.pauseCh:
				a.isPaused = true
			case <-time.After(a.pollFrequency):
				a.updateClusterAvailabilityMetrics(k8sClient)
				if a.hostLevelAvailabilityEnabled() {
					a.updateHostAvailabilityMetrics()
				}
			case <-a.stopCh:
				return
			}
		}
	}()
	return nil
}

func (a *apiAvailabilityMeasurement) initFields(config *measurement.Config) (err error) {
	a.isRunning = true
	a.stopCh = make(chan struct{})
	a.pauseCh = make(chan struct{})
	a.unpauseCh = make(chan struct{})
	frequency, err := util.GetDuration(config.Params, "pollFrequency")
	if err != nil {
		return err
	}
	a.pollFrequency = frequency

	threshold, err := util.GetFloat64OrDefault(config.Params, "threshold", 0.0)
	if err != nil {
		return err
	}
	a.threshold = threshold

	a.clusterLevelMetrics = &apiAvailabilityMetrics{}

	a.useHostInternalIPs, err = util.GetBoolOrDefault(config.Params, "useHostInternalIPs", config.ClusterLoaderConfig.ExecServiceConfig.Enable && len(config.ClusterFramework.GetClusterConfig().MasterInternalIPs) != 0)
	if err != nil {
		return err
	}
	a.useHostPublicIPs, err = util.GetBoolOrDefault(config.Params, "useHostPublicIPs", false)
	if err != nil {
		return err
	}
	if a.useHostInternalIPs || a.useHostPublicIPs {
		err = a.addHostIPs(config)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *apiAvailabilityMeasurement) addHostIPs(config *measurement.Config) error {
	if config.ClusterLoaderConfig.ExecServiceConfig.Enable {
		if a.useHostInternalIPs {
			if len(config.ClusterFramework.GetClusterConfig().MasterInternalIPs) == 0 {
				return fmt.Errorf("%s: host internal IP(s) are not provided, cannot measure availability through internal IPs", a)
			}
			a.hostIPs = config.ClusterFramework.GetClusterConfig().MasterInternalIPs
		}
		if a.useHostPublicIPs {
			if len(config.ClusterFramework.GetClusterConfig().MasterIPs) == 0 {
				return fmt.Errorf("%s: host public IP(s) are not provided, cannot measure availability through public IPs", a)
			}
			a.hostIPs = append(a.hostIPs, config.ClusterFramework.GetClusterConfig().MasterIPs...)
		}
		a.hostLevelMetrics = map[string]*apiAvailabilityMetrics{}
		for _, ip := range a.hostIPs {
			a.hostLevelMetrics[ip] = &apiAvailabilityMetrics{}
		}
		hostPollTimeoutSeconds, err := util.GetIntOrDefault(config.Params, "hostPollTimeoutSeconds", 5)
		if err != nil {
			return err
		}
		a.hostPollTimeoutSeconds = hostPollTimeoutSeconds
		hostPollExecTimeoutSeconds, err := util.GetIntOrDefault(config.Params, "hostPollExecTimeoutSeconds", 10)
		if err != nil {
			return err
		}
		a.hostPollExecTimeoutSeconds = hostPollExecTimeoutSeconds
	} else {
		return fmt.Errorf("%s: exec service is not enabled, cannot measure availability through host IPs", a)
	}
	return nil
}

func (a *apiAvailabilityMeasurement) hostLevelAvailabilityEnabled() bool {
	return len(a.hostLevelMetrics) > 0
}

func (a *apiAvailabilityMeasurement) pause() {
	if !a.isRunning {
		klog.V(2).Infof("%s: measurement is not running", a)
		return
	}
	if a.isPaused {
		klog.Warningf("%s: measurement already paused", a)
		return
	}
	a.pauseCh <- struct{}{}
	klog.V(2).Infof("%s: pausing the measurement (stopping checking the availability)", a)
}

func (a *apiAvailabilityMeasurement) unpause() {
	if !a.isRunning {
		klog.V(2).Infof("%s: measurement is not running", a)
		return
	}
	if !a.isPaused {
		klog.Warningf("%s: measurement already unpaused", a)
		return
	}
	a.unpauseCh <- struct{}{}
	klog.V(2).Infof("%s: unpausing the measurement", a)
}

func (a *apiAvailabilityMeasurement) gather() ([]measurement.Summary, error) {
	if !a.isRunning {
		return nil, nil
	}
	close(a.stopCh)
	a.wg.Wait()
	a.isRunning = false
	klog.V(2).Infof("%s: gathering summaries", apiAvailabilityMeasurementName)

	output := apiAvailabilityOutput{
		ClusterSummary: createClusterSummary(a.clusterLevelMetrics, a.pollFrequency),
	}
	if a.hostLevelAvailabilityEnabled() {
		output.HostSummaries = createHostSummary(a.hostLevelMetrics, a.hostIPs, a.pollFrequency)
	}

	content, err := util.PrettyPrintJSON(output)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(apiAvailabilityMeasurementName, "json", content)
	a.summaries = append(a.summaries, summary)
	if sli := output.ClusterSummary.AvailabilityPercentage; sli < a.threshold {
		err = errors.NewMetricViolationError("API availability", fmt.Sprintf("SLO not fulfilled (expected >= %.2f, got: %.2f)", a.threshold, sli))
	}
	return a.summaries, err
}

func (a *apiAvailabilityMeasurement) Dispose() {}

func (a *apiAvailabilityMeasurement) String() string {
	return apiAvailabilityMeasurementName
}
