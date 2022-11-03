/*
Copyright 2022 The Kubernetes Authors.

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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/chaos"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	chaosMonkeyMeasurementName = "ChaosMonkey"
)

func init() {
	create := func() measurement.Measurement {
		return &chaosMonkeyMeasurement{
			killedNodes: sets.NewString(),
		}
	}
	if err := measurement.Register(chaosMonkeyMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", chaosMonkeyMeasurementName, err)
	}
}

type chaosMonkeyMeasurement struct {
	api.NodeFailureConfig
	stopChannel          chan struct{}
	chaosMonkey          *chaos.Monkey
	chaosMonkeyWaitGroup *sync.WaitGroup
	killedNodes          sets.String
}

func (c *chaosMonkeyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if err := util.ToStruct(config.Params, &c.NodeFailureConfig); err != nil {
			return nil, err
		}
		c.stopChannel = make(chan struct{})
		c.chaosMonkey = chaos.NewMonkey(config.ClusterFramework.GetClientSets().GetClient(), config.CloudProvider)
		c.chaosMonkeyWaitGroup, err = c.chaosMonkey.Init(api.ChaosMonkeyConfig{&c.NodeFailureConfig, c.killedNodes}, c.stopChannel)
		if err != nil {
			close(c.stopChannel)
			return nil, fmt.Errorf("error while creating chaos monkey: %v", err)
		}
		return nil, nil
	case "stop":
		close(c.stopChannel)
		if c.chaosMonkeyWaitGroup != nil {
			// Wait for the Chaos Monkey subroutine to end
			klog.V(2).Info("Waiting for the chaos monkey subroutine to end...")
			c.chaosMonkeyWaitGroup.Wait()
			klog.V(2).Info("Chaos monkey ended.")
		}
		c.killedNodes = c.chaosMonkey.KilledNodes()
		klog.V(2).Infof(c.chaosMonkey.Summary())
		// ChaosMonkey doesn't collect metrics and returns empty measurement.Summary.
		return []measurement.Summary{}, nil
	default:
		return nil, fmt.Errorf("unknown action: %v", action)
	}
}

func (c *chaosMonkeyMeasurement) Dispose() {}

func (c *chaosMonkeyMeasurement) String() string {
	return chaosMonkeyMeasurementName
}
