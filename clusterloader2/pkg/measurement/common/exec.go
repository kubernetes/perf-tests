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
	"os"
	"os/exec"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	execName            = "Exec"
	defaultTimeout      = 1 * time.Hour
	defaultBackoffDelay = 1 * time.Second
)

func init() {
	if err := measurement.Register(execName, createExecMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", execName, err)
	}
}

func createExecMeasurement() measurement.Measurement {
	return &execMeasurement{}
}

type execMeasurement struct{}

func (e *execMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultTimeout)
	if err != nil {
		return nil, err
	}
	command, err := util.GetStringArray(config.Params, "command")
	if err != nil {
		return nil, fmt.Errorf("error parsing command: %v", err)
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("command is a required argument. Got empty slice instead")
	}
	retries, err := util.GetIntOrDefault(config.Params, "retries", 1)
	if err != nil || retries < 1 {
		return nil, fmt.Errorf("error getting retries, retries: %v, err: %v", retries, err)
	}
	backoffDelay, err := util.GetDurationOrDefault(config.Params, "backoffDelay", defaultBackoffDelay)
	if err != nil {
		return nil, err
	}
	// Make a copy of command, to avoid overriding a slice we don't own.
	command = append([]string{}, command...)
	for i := range command {
		command[i] = os.ExpandEnv(command[i])
	}
	var lastErr error
	for i := 0; i < retries; i++ {
		klog.V(2).Infof("Running %v with timeout %v, attempt %v", command, timeout, i+1)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		out, err := cmd.CombinedOutput()
		klog.V(2).Infof("Exec command output: %v", string(out))
		if err == nil {
			klog.V(2).Infof("Command %v succeeded in attempt %v", command, i+1)
			return nil, nil
		}
		klog.V(2).Infof("Command %v failed in attempt %v: %v", command, i+1, err)
		lastErr = err
		time.Sleep(backoffDelay)
	}
	// All attempts failed.
	return nil, fmt.Errorf("command %v failed: %v", command, lastErr)
}

func (e *execMeasurement) Dispose() {}

func (e *execMeasurement) String() string { return execName }
