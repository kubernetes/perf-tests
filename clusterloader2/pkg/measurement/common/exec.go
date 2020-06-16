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

	"k8s.io/klog"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	execName = "Exec"
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

func (e *execMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	timeoutStr, err := util.GetStringOrDefault(config.Params, "timeout", "1h")
	if err != nil {
		return nil, err
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return nil, err
	}
	name, err := util.GetString(config.Params, "name")
	if err != nil {
		return nil, err
	}
	args, err := util.GetArray(config.Params, "args")
	if err != nil {
		return nil, err
	}
	for i := range args {
		args[i] = os.ExpandEnv(args[i])
	}
	klog.Infof("Running %q %v with timeout %v", name, args, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	klog.Infof("output: %v", string(out))
	if err != nil {
		return nil, fmt.Errorf("command %q %v failed: %v", name, args, err)
	}
	return nil, nil
}

func (s *execMeasurement) Dispose() {}

func (s *execMeasurement) String() string { return execName }
