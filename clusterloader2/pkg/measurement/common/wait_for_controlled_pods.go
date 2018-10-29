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

/*
TODO(krzysied): Replace ClientSet interface with dynamic client.
This will allow more generic approach.
*/

package common

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	defaultWaitForRCPodsTimeout      = 60 * time.Second
	defaultWaitForRCPodsInterval     = 5 * time.Second
	waitForControlledPodsRunningName = "WaitForControlledPodsRunning"
)

func init() {
	measurement.Register(waitForControlledPodsRunningName, createWaitForControlledPodsRunningMeasurement)
}

func createWaitForControlledPodsRunningMeasurement() measurement.Measurement {
	return &waitForControlledPodsRunningMeasurement{}
}

type waitForControlledPodsRunningMeasurement struct{}

// Execute waits until all specified RCs have all pods running or until timeout happens.
// RCs can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
func (*waitForControlledPodsRunningMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	var summaries []measurement.Summary
	kind, err := util.GetString(config.Params, "kind")
	if err != nil {
		return summaries, err
	}
	namespace, err := util.GetStringOrDefault(config.Params, "namespace", metav1.NamespaceAll)
	if err != nil {
		return summaries, err
	}
	labelSelector, err := util.GetStringOrDefault(config.Params, "labelSelector", "")
	if err != nil {
		return summaries, err
	}
	fieldSelector, err := util.GetStringOrDefault(config.Params, "fieldSelector", "")
	if err != nil {
		return summaries, err
	}
	timeout, err := util.GetDurationOrDefault(config.Params, "timeout", defaultWaitForRCPodsTimeout)
	if err != nil {
		return summaries, err
	}

	runtimeObjectsList, err := runtimeobjects.ListRuntimeObjectsForKind(config.ClientSet, kind, namespace, labelSelector, fieldSelector)
	if err != nil {
		return summaries, err
	}
	var wg wait.Group
	errList := util.NewErrorList()
	var runningRuntimeObjects int32
	for objectCounter := 0; objectCounter < len(runtimeObjectsList); objectCounter++ {
		objectIndex := objectCounter
		wg.Start(func() {
			if err := waitForRuntimeObject(config.ClientSet, runtimeObjectsList[objectIndex], timeout); err != nil {
				errList.Append(fmt.Errorf("waiting for %v error: %v", kind, err))
				return
			}

			atomic.AddInt32(&runningRuntimeObjects, 1)
			objName, err := runtimeobjects.GetNameFromRuntimeObject(runtimeObjectsList[objectIndex])
			if err != nil {
				errList.Append(fmt.Errorf("reading object name error: %v", err))
				return
			}
			objNamespace, err := runtimeobjects.GetNamespaceFromRuntimeObject(runtimeObjectsList[objectIndex])
			if err != nil {
				errList.Append(fmt.Errorf("reading object namespace error: %v", err))
				return
			}
			glog.Infof("%s: %s (%s) has all pods running", waitForControlledPodsRunningName, objName, objNamespace)
		})
	}
	wg.Wait()

	if int(runningRuntimeObjects) == len(runtimeObjectsList) {
		glog.Infof("%s: %d / %d %ss are running with all pods", waitForControlledPodsRunningName, runningRuntimeObjects, len(runtimeObjectsList), kind)
		return summaries, nil
	}
	return summaries, fmt.Errorf("error while waiting for %ss to be running in namespace '%v' with labels '%v' and fields '%v' - only %d found running with all pods: %v",
		kind, namespace, labelSelector, fieldSelector, runningRuntimeObjects, errList.String())
}

func waitForRuntimeObject(c clientset.Interface, obj runtime.Object, timeout time.Duration) error {
	runtimeObjectNamespace, err := runtimeobjects.GetNamespaceFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	runtimeObjectSelector, err := runtimeobjects.GetSelectorFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	runtimeObjectReplicas, err := runtimeobjects.GetReplicasFromRuntimeObject(obj)
	if err != nil {
		return err
	}

	err = waitForPods(c, runtimeObjectNamespace, runtimeObjectSelector.String(), "", int(runtimeObjectReplicas), timeout, false)
	if err != nil {
		return fmt.Errorf("waiting pods error: %v", err)
	}
	return nil
}
