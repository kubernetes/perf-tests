/*
Copyright 2021 The Kubernetes Authors.

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

package network

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

func (npm *networkPerformanceMeasurement) validate(config *measurement.Config) error {
	var err error
	if npm.testDuration, err = util.GetDuration(config.Params, "duration"); err != nil {
		return err
	}
	if npm.protocol, err = util.GetString(config.Params, "protocol"); err != nil {
		return err
	}
	if npm.protocol != protocolTCP && npm.protocol != protocolUDP && npm.protocol != protocolHTTP {
		return fmt.Errorf("invalid protocol , supported ones are TCP,UDP,HTTP")
	}
	if err = npm.validatePodConfiguration(config); err != nil {
		return err
	}
	npm.podRatioType = npm.getPodRatioType()
	return nil
}

func (npm *networkPerformanceMeasurement) validatePodConfiguration(config *measurement.Config) error {
	var err error
	if npm.numberOfClients, err = util.GetInt(config.Params, "numberOfClients"); err != nil {
		return err
	}
	if npm.numberOfServers, err = util.GetInt(config.Params, "numberOfServers"); err != nil {
		return err
	}
	if npm.numberOfClients <= 0 || npm.numberOfServers <= 0 {
		return fmt.Errorf("invalid number of client or server pods")
	}
	return nil
}

func (npm *networkPerformanceMeasurement) getPodRatioType() string {
	if npm.numberOfClients == npm.numberOfServers && npm.numberOfClients == 1 {
		return oneToOne
	}
	if (npm.numberOfClients > npm.numberOfServers) && npm.numberOfServers == 1 {
		return manyToOne
	}
	if npm.numberOfClients == npm.numberOfServers {
		return manyToMany
	}
	return invalid
}

func (npm *networkPerformanceMeasurement) populateTemplate(podPair podPair, index int) map[string]interface{} {
	return map[string]interface{}{
		"index":                index,
		"clientPodName":        podPair.sourcePodName,
		"serverPodName":        podPair.destinationPodName,
		"clientPodIP":          podPair.sourcePodIP,
		"serverPodIP":          podPair.destinationPodIP,
		"protocol":             npm.protocol,
		"duration":             npm.testDuration.Seconds(),
		"clientStartTimestamp": npm.startTimeStampForTestExecution,
		"numberOfClients":      1,
	}
}

func getInformer(namespace string, k8sClient dynamic.Interface, gvr schema.GroupVersionResource) (cache.SharedInformer, error) {
	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(k8sClient, 0, namespace, nil)
	informer := dynamicInformerFactory.ForResource(gvr).Informer()
	return informer, nil
}

type metricPercentiles struct {
	percentile05 float64
	percentile50 float64
	percentile95 float64
}

type float64Slice []float64

func (p float64Slice) Len() int           { return len(p) }
func (p float64Slice) Less(i, j int) bool { return p[i] < p[j] }
func (p float64Slice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func getPercentile(values float64Slice) metricPercentiles {
	length := len(values)
	if length == 0 {
		return metricPercentiles{0, 0, 0}
	}
	sort.Float64s(values)
	perc05 := values[int(math.Ceil(float64(length*05)/100))-1]
	perc50 := values[int(math.Ceil(float64(length*50)/100))-1]
	perc95 := values[int(math.Ceil(float64(length*95)/100))-1]
	return metricPercentiles{percentile05: perc05, percentile50: perc50, percentile95: perc95}
}

func getMetricResponse(metricMap map[string]interface{}, response *metricResponse) error {
	body, err := json.Marshal(metricMap)
	if err != nil {
		return fmt.Errorf("error marshaling metricMap : %s", err)
	}
	if err = json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("error un-marshaling onto metricResponse: %s", err)
	}
	return nil
}
