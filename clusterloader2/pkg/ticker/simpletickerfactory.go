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

package ticker

import (
	"fmt"

	"k8s.io/perf-tests/clusterloader2/api"
)

type simpleTickerFactory struct {
	tuningSetMap map[string]*api.TuningSet
}

// NewTickerFactory creates new ticker factory.
func NewTickerFactory() TickerFactory {
	return &simpleTickerFactory{
		tuningSetMap: make(map[string]*api.TuningSet),
	}
}

// Init sets available tuning sets.
func (tf *simpleTickerFactory) Init(tuningSets []api.TuningSet) {
	tf.tuningSetMap = make(map[string]*api.TuningSet)
	for i := range tuningSets {
		tf.tuningSetMap[tuningSets[i].Name] = &tuningSets[i]
	}
}

// CreateTicker creates new ticker based on provided tuning set name.
func (tf *simpleTickerFactory) CreateTicker(name string) (*ticker, error) {
	tuningSet, exists := tf.tuningSetMap[name]
	if !exists {
		return nil, fmt.Errorf("tuningset %s not found", name)
	}
	return newTicker(tuningSet), nil
}
