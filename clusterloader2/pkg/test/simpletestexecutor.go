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

package test

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
)

const (
	basenamePlaceholder = "Basename"
	indexPlaceholder    = "Index"
)

type simpleTestExecutor struct{}

func createSimpleTestExecutor() TestExecutor {
	return &simpleTestExecutor{}
}

// ExecuteTest executes test based on provided configuration.
func (ste *simpleTestExecutor) ExecuteTest(ctx Context, conf *api.Config) []error {
	defer cleanupResources(ctx)
	var errList []error
	ctx.GetTickerFactory().Init(conf.TuningSets)
	err := ctx.GetFramework().CreateAutomanagedNamespaces(int(conf.AutomanagedNamespaces))
	if err != nil {
		errList = append(errList, fmt.Errorf("automanaged namespaces creation failed: %v", err))
		return errList
	}

	for i := range conf.Steps {
		if stepErrList := ste.ExecuteStep(ctx, &conf.Steps[i]); len(stepErrList) > 0 {
			errList = append(errList, stepErrList...)
			if isErrsCritical(stepErrList) {
				return errList
			}
		}
	}
	return errList
}

// ExecuteStep executes single test step based on provided step configuration.
func (ste *simpleTestExecutor) ExecuteStep(ctx Context, step *api.Step) []error {
	var wg wait.Group
	// TODO(krzysied): Consider moving lock and errList to separate structure.
	var lock sync.Mutex
	var errList []error
	if len(step.Measurements) > 0 {
		for i := range step.Measurements {
			// index is created to make i value unchangeable during thread execution.
			index := i
			wg.Start(func() {
				defer func() {
					if r := recover(); r != nil {
						lock.Lock()
						defer lock.Unlock()
						errList = append(errList, fmt.Errorf("measurement call %s - %s panic: %v", step.Measurements[index].Method, step.Measurements[index].Identifier, r))
					}
				}()
				if err := ctx.GetMeasurementManager().Execute(step.Measurements[index].Method, step.Measurements[index].Identifier, step.Measurements[index].Params); err != nil {
					lock.Lock()
					defer lock.Unlock()
					errList = append(errList, fmt.Errorf("measurement call %s - %s error: %v", step.Measurements[index].Method, step.Measurements[index].Identifier, err))
				}
			})
		}
	} else {
		for i := range step.Phases {
			phase := &step.Phases[i]
			wg.Start(func() {
				if phaseErrList := ste.ExecutePhase(ctx, phase); len(phaseErrList) > 0 {
					lock.Lock()
					defer lock.Unlock()
					errList = append(errList, phaseErrList...)
				}
			})
		}
	}
	wg.Wait()
	return errList
}

// ExecutePhase executes single test phase based on provided phase configuration.
func (ste *simpleTestExecutor) ExecutePhase(ctx Context, phase *api.Phase) []error {
	// TODO: add tuning set
	var errList []error
	nsList := createNamespacesList(phase.NamespaceRange)
	ticker, err := ctx.GetTickerFactory().CreateTicker(phase.TuningSet)
	if err != nil {
		return []error{fmt.Errorf("ticker creation error: %v", err)}
	}
	defer ticker.Stop()
	for _, nsName := range nsList {
		instancesStates := make([]*state.InstancesState, 0)
		// Updating state (DesiredReplicaCount) of every object in object bundle.
		for j := range phase.ObjectBundle {
			id, err := getIdentifier(ctx, &phase.ObjectBundle[j])
			if err != nil {
				errList = append(errList, err)
				return errList
			}
			instances, exists := ctx.GetState().Get(nsName, id)
			if !exists {
				instances = &state.InstancesState{
					DesiredReplicaCount: 0,
					CurrentReplicaCount: 0,
					Object:              phase.ObjectBundle[j],
				}
			}
			instances.DesiredReplicaCount = phase.ReplicasPerNamespace
			ctx.GetState().Set(nsName, id, instances)
			instancesStates = append(instancesStates, instances)
		}

		// Calculating maximal replica count of objects from object bundle.
		var maxCurrentReplicaCount int32
		for j := range instancesStates {
			if instancesStates[j].CurrentReplicaCount > maxCurrentReplicaCount {
				maxCurrentReplicaCount = instancesStates[j].CurrentReplicaCount
			}
		}
		// Deleting objects with index greater or equal requested replicas per namespace number.
		// Objects will be deleted in reversed order.
		for replicaIndex := phase.ReplicasPerNamespace; replicaIndex < maxCurrentReplicaCount; replicaIndex++ {
			for j := len(phase.ObjectBundle) - 1; j >= 0; j-- {
				if replicaIndex < instancesStates[j].CurrentReplicaCount {
					<-ticker.C
					if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, DELETE_OBJECT); len(objectErrList) > 0 {
						errList = append(errList, objectErrList...)
						if isErrsCritical(objectErrList) {
							return errList
						}
					}
				}
			}
		}
		// Handling for update/create objects.
		for replicaIndex := int32(0); replicaIndex < phase.ReplicasPerNamespace; replicaIndex++ {
			for j := range phase.ObjectBundle {
				if instancesStates[j].CurrentReplicaCount == phase.ReplicasPerNamespace {
					<-ticker.C
					if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, PATCH_OBJECT); len(objectErrList) > 0 {
						errList = append(errList, objectErrList...)
						if isErrsCritical(objectErrList) {
							return errList
						}
						// If error then skip this bundle
						break
					}
				} else if replicaIndex >= instancesStates[j].CurrentReplicaCount {
					<-ticker.C
					if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, CREATE_OBJECT); len(objectErrList) > 0 {
						errList = append(errList, objectErrList...)
						if isErrsCritical(objectErrList) {
							return errList
						}
						// If error then skip this bundle
						break
					}
				}
			}
		}
		// Updating state (CurrentReplicaCount) of every object in object bundle.
		for j := range phase.ObjectBundle {
			id, _ := getIdentifier(ctx, &phase.ObjectBundle[j])
			instancesStates[j].CurrentReplicaCount = instancesStates[j].DesiredReplicaCount
			ctx.GetState().Set(nsName, id, instancesStates[j])
		}
	}
	return errList
}

// ExecuteObject executes single test object operation based on provided object configuration.
func (ste *simpleTestExecutor) ExecuteObject(ctx Context, object *api.Object, namespace string, replicaIndex int32, operation OperationType) []error {
	var errList []error
	objName := fmt.Sprintf("%v-%d", object.Basename, replicaIndex)
	var mapping map[string]interface{}
	if object.TemplateFillMap == nil {
		mapping = make(map[string]interface{})
	} else {
		mapping = object.TemplateFillMap
	}
	mapping[basenamePlaceholder] = objName
	mapping[indexPlaceholder] = replicaIndex
	obj, err := ctx.GetTemplateProvider().TemplateToObject(object.ObjectTemplatePath, mapping)
	if err != nil {
		return []error{fmt.Errorf("reading template (%v) error: %v", object.ObjectTemplatePath, err)}
	}
	gvk := obj.GroupVersionKind()

	if namespace == "" {
		// TODO: handle cluster level object
	} else {
		switch operation {
		case CREATE_OBJECT:
			if err := ctx.GetFramework().CreateObject(namespace, objName, obj); err != nil {
				errList = append(errList, fmt.Errorf("namespace %v object %v creation error: %v", namespace, objName, err))
			}
		case PATCH_OBJECT:
			if err := ctx.GetFramework().PatchObject(namespace, objName, obj); err != nil {
				errList = append(errList, fmt.Errorf("namespace %v object %v updating error: %v", namespace, objName, err))
			}
		case DELETE_OBJECT:
			if err := ctx.GetFramework().DeleteObject(gvk, namespace, objName); err != nil {
				errList = append(errList, fmt.Errorf("namespace %v object %v deletion error: %v", namespace, objName, err))
			}
		default:
			errList = append(errList, fmt.Errorf("Unsupported operation %v for namespace %v object %v", operation, namespace, objName))
		}
	}
	return errList
}

func getIdentifier(ctx Context, object *api.Object) (state.InstancesIdentifier, error) {
	obj, err := ctx.GetTemplateProvider().TemplateToObject(object.ObjectTemplatePath, nil)
	if err != nil {
		return state.InstancesIdentifier{}, fmt.Errorf("reading template (%v) error: %v", object.ObjectTemplatePath, err)
	}
	gvk := obj.GroupVersionKind()
	return state.InstancesIdentifier{
		Basename:   object.Basename,
		ObjectKind: gvk.Kind,
		ApiGroup:   gvk.Group,
	}, nil
}

func createNamespacesList(namespaceRange *api.NamespaceRange) []string {
	if namespaceRange == nil {
		return []string{""}
	}

	nsList := make([]string, 0)
	nsBasename := framework.AutomanagedNamespaceName
	if namespaceRange.Basename != nil {
		nsBasename = *namespaceRange.Basename
	}

	for i := namespaceRange.Min; i <= namespaceRange.Max; i++ {
		nsList = append(nsList, fmt.Sprintf("%v-%d", nsBasename, i))
	}
	return nsList
}

func isErrsCritical(errList []error) bool {
	// TODO: define critical errors
	return false
}

func cleanupResources(ctx Context) {
	if err := ctx.GetFramework().DeleteAutomanagedNamespaces(); err != nil {
		glog.Errorf("Resource cleanup error: %v", err)
	}
}
