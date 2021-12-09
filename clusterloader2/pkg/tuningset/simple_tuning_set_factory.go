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

package tuningset

import (
	"fmt"

	"k8s.io/perf-tests/clusterloader2/api"
)

type simpleFactory struct {
	tuningSetMap         map[string]*api.TuningSet
	globalQPSLoadFactory *globalQPSLoadFactory
}

// NewFactory creates new ticker factory.
func NewFactory() Factory {
	return &simpleFactory{
		tuningSetMap:         make(map[string]*api.TuningSet),
		globalQPSLoadFactory: newGlobalQPSLoadFactory(),
	}
}

// Init sets available tuning sets.
func (tf *simpleFactory) Init(tuningSets []*api.TuningSet) {
	tf.tuningSetMap = make(map[string]*api.TuningSet)
	for i := range tuningSets {
		tf.tuningSetMap[tuningSets[i].Name] = tuningSets[i]
	}
}

// CreateTuningSet creates new tuning set based on provided tuning set name.
func (tf *simpleFactory) CreateTuningSet(name string) (TuningSet, error) {
	tuningSet, exists := tf.tuningSetMap[name]
	if !exists {
		return nil, fmt.Errorf("tuningset %s not found", name)
	}
	switch {
	case tuningSet.QPSLoad != nil:
		return newQPSLoad(tuningSet.QPSLoad), nil
	case tuningSet.RandomizedLoad != nil:
		return newRandomizedLoad(tuningSet.RandomizedLoad), nil
	case tuningSet.SteppedLoad != nil:
		return newSteppedLoad(tuningSet.SteppedLoad), nil
	case tuningSet.TimeLimitedLoad != nil:
		return newTimeLimitedLoad(tuningSet.TimeLimitedLoad), nil
	case tuningSet.RandomizedTimeLimitedLoad != nil:
		return newRandomizedTimeLimitedLoad(tuningSet.RandomizedTimeLimitedLoad), nil
	case tuningSet.ParallelismLimitedLoad != nil:
		return newParallelismLimitedLoad(tuningSet.ParallelismLimitedLoad), nil
	case tuningSet.GlobalQPSLoad != nil:
		return tf.globalQPSLoadFactory.GetOrCreate(name, tuningSet.GlobalQPSLoad), nil
	default:
		return nil, fmt.Errorf("incorrect tuning set: %v", tuningSet)
	}
}
