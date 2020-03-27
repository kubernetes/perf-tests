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
	"io/ioutil"
	"path"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/state"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	baseNamePlaceholder  = "BaseName"
	indexPlaceholder     = "Index"
	namePlaceholder      = "Name"
	namespacePlaceholder = "Namespace"
)

type simpleTestExecutor struct{}

func createSimpleTestExecutor() TestExecutor {
	return &simpleTestExecutor{}
}

// ExecuteTest executes test based on provided configuration.
func (ste *simpleTestExecutor) ExecuteTest(ctx Context, conf *api.Config) *errors.ErrorList {
	ctx.GetClusterFramework().SetAutomanagedNamespacePrefix(fmt.Sprintf("test-%s", util.RandomDNS1123String(6)))
	klog.Infof("AutomanagedNamespacePrefix: %s", ctx.GetClusterFramework().GetAutomanagedNamespacePrefix())
	defer cleanupResources(ctx)
	ctx.GetTuningSetFactory().Init(conf.TuningSets)

	stopCh := make(chan struct{})
	chaosMonkeyWaitGroup, err := ctx.GetChaosMonkey().Init(conf.ChaosMonkey, stopCh)
	if err != nil {
		close(stopCh)
		return errors.NewErrorList(fmt.Errorf("error while creating chaos monkey: %v", err))
	}
	errList := ste.ExecuteTestSteps(ctx, conf)
	close(stopCh)

	if chaosMonkeyWaitGroup != nil {
		// Wait for the Chaos Monkey subroutine to end
		klog.Info("Waiting for the chaos monkey subroutine to end...")
		chaosMonkeyWaitGroup.Wait()
		klog.Info("Chaos monkey ended.")
	}

	for _, summary := range ctx.GetMeasurementManager().GetSummaries() {
		if ctx.GetClusterLoaderConfig().ReportDir == "" {
			klog.Infof("%v: %v", summary.SummaryName(), summary.SummaryContent())
		} else {
			testDistinctor := ""
			if ctx.GetClusterLoaderConfig().TestScenario.Identifier != "" {
				testDistinctor = "_" + ctx.GetClusterLoaderConfig().TestScenario.Identifier
			}
			// TODO(krzysied): Remember to keep original filename style for backward compatibility.
			fileName := strings.Join([]string{summary.SummaryName(), conf.Name + testDistinctor, summary.SummaryTime().Format(time.RFC3339)}, "_")
			filePath := path.Join(ctx.GetClusterLoaderConfig().ReportDir, strings.Join([]string{fileName, summary.SummaryExt()}, "."))
			if err := ioutil.WriteFile(filePath, []byte(summary.SummaryContent()), 0644); err != nil {
				errList.Append(fmt.Errorf("writing to file %v error: %v", filePath, err))
				continue
			}
		}
	}
	klog.Infof(ctx.GetChaosMonkey().Summary())
	return errList
}

// ExecuteTestSteps executes all test steps provided in configuration
func (ste *simpleTestExecutor) ExecuteTestSteps(ctx Context, conf *api.Config) *errors.ErrorList {
	automanagedNamespacesList, staleNamespaces, err := ctx.GetClusterFramework().ListAutomanagedNamespaces()
	if err != nil {
		return errors.NewErrorList(fmt.Errorf("automanaged namespaces listing failed: %v", err))
	}
	if len(automanagedNamespacesList) > 0 {
		return errors.NewErrorList(fmt.Errorf("pre-existing automanaged namespaces found"))
	}
	var deleteStaleNS = ctx.GetClusterFramework().GetClusterConfig().DeleteStaleNamespaces
	if len(staleNamespaces) > 0 && deleteStaleNS {
		klog.Warning("stale automanaged namespaces found")
		if errList := ctx.GetClusterFramework().DeleteNamespaces(staleNamespaces); !errList.IsEmpty() {
			klog.Errorf("stale automanaged namespaces cleanup error: %v", errList.String())
		}
	}

	err = ctx.GetClusterFramework().CreateAutomanagedNamespaces(int(conf.AutomanagedNamespaces))
	if err != nil {
		return errors.NewErrorList(fmt.Errorf("automanaged namespaces creation failed: %v", err))
	}

	errList := errors.NewErrorList()
	for i := range conf.Steps {
		if stepErrList := ste.ExecuteStep(ctx, &conf.Steps[i]); !stepErrList.IsEmpty() {
			errList.Concat(stepErrList)
			if isErrsCritical(stepErrList) {
				return errList
			}
		}
	}
	return errList
}

// ExecuteStep executes single test step based on provided step configuration.
func (ste *simpleTestExecutor) ExecuteStep(ctx Context, step *api.Step) *errors.ErrorList {
	if step.Name != "" {
		klog.Infof("Step %q started", step.Name)
	}
	var wg wait.Group
	errList := errors.NewErrorList()
	if len(step.Measurements) > 0 {
		for i := range step.Measurements {
			// index is created to make i value unchangeable during thread execution.
			index := i
			wg.Start(func() {
				err := ctx.GetMeasurementManager().Execute(step.Measurements[index].Method,
					step.Measurements[index].Identifier,
					step.Measurements[index].Params)
				if err != nil {
					errList.Append(fmt.Errorf("measurement call %s - %s error: %v", step.Measurements[index].Method, step.Measurements[index].Identifier, err))
				}
			})
		}
	} else {
		for i := range step.Phases {
			phase := &step.Phases[i]
			wg.Start(func() {
				if phaseErrList := ste.ExecutePhase(ctx, phase); !phaseErrList.IsEmpty() {
					errList.Concat(phaseErrList)
				}
			})
		}
	}
	wg.Wait()
	if step.Name != "" {
		klog.Infof("Step %q ended", step.Name)
	}
	if !errList.IsEmpty() {
		klog.Warningf("Got errors during step execution: %v", errList)
	}
	return errList
}

// ExecutePhase executes single test phase based on provided phase configuration.
func (ste *simpleTestExecutor) ExecutePhase(ctx Context, phase *api.Phase) *errors.ErrorList {
	// TODO: add tuning set
	errList := errors.NewErrorList()
	nsList := createNamespacesList(ctx, phase.NamespaceRange)
	tuningSet, err := ctx.GetTuningSetFactory().CreateTuningSet(phase.TuningSet)
	if err != nil {
		return errors.NewErrorList(fmt.Errorf("tuning set creation error: %v", err))
	}

	var actions []func()
	for namespaceIndex := range nsList {
		nsName := nsList[namespaceIndex]
		instancesStates := make([]*state.InstancesState, 0)
		// Updating state (DesiredReplicaCount) of every object in object bundle.
		for j := range phase.ObjectBundle {
			id, err := getIdentifier(ctx, &phase.ObjectBundle[j])
			if err != nil {
				errList.Append(err)
				return errList
			}
			instances, exists := ctx.GetState().GetNamespacesState().Get(nsName, id)
			if !exists {
				currentReplicaCount, err := getReplicaCountOfNewObject(ctx, nsName, &phase.ObjectBundle[j])
				if err != nil {
					errList.Append(err)
					return errList
				}
				instances = &state.InstancesState{
					DesiredReplicaCount: 0,
					CurrentReplicaCount: currentReplicaCount,
					Object:              phase.ObjectBundle[j],
				}
			}
			instances.DesiredReplicaCount = phase.ReplicasPerNamespace
			ctx.GetState().GetNamespacesState().Set(nsName, id, instances)
			instancesStates = append(instancesStates, instances)
		}

		if err := verifyBundleCorrectness(instancesStates); err != nil {
			klog.Errorf("Skipping phase. Incorrect bundle in phase: %+v", *phase)
			return errors.NewErrorList(err)
		}

		// Deleting objects with index greater or equal requested replicas per namespace number.
		// Objects will be deleted in reversed order.
		for replicaCounter := phase.ReplicasPerNamespace; replicaCounter < instancesStates[0].CurrentReplicaCount; replicaCounter++ {
			replicaIndex := replicaCounter
			actions = append(actions, func() {
				for j := len(phase.ObjectBundle) - 1; j >= 0; j-- {
					if replicaIndex < instancesStates[j].CurrentReplicaCount {
						if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, DELETE_OBJECT); !objectErrList.IsEmpty() {
							errList.Concat(objectErrList)
						}
					}
				}
			})
		}

		// Updating objects when desired replicas per namespace equals current replica count.
		if instancesStates[0].CurrentReplicaCount == phase.ReplicasPerNamespace {
			for replicaCounter := int32(0); replicaCounter < phase.ReplicasPerNamespace; replicaCounter++ {
				replicaIndex := replicaCounter
				actions = append(actions, func() {
					for j := range phase.ObjectBundle {
						if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, PATCH_OBJECT); !objectErrList.IsEmpty() {
							errList.Concat(objectErrList)
							// If error then skip this bundle
							break
						}
					}
				})
			}
		}

		// Adding objects with index greater than current replica count and lesser than desired replicas per namespace.
		for replicaCounter := instancesStates[0].CurrentReplicaCount; replicaCounter < phase.ReplicasPerNamespace; replicaCounter++ {
			replicaIndex := replicaCounter
			actions = append(actions, func() {
				for j := range phase.ObjectBundle {
					if objectErrList := ste.ExecuteObject(ctx, &phase.ObjectBundle[j], nsName, replicaIndex, CREATE_OBJECT); !objectErrList.IsEmpty() {
						errList.Concat(objectErrList)
						// If error then skip this bundle
						break
					}
				}
			})
		}

		// Updating state (CurrentReplicaCount) of every object in object bundle.
		defer func() {
			for j := range phase.ObjectBundle {
				id, _ := getIdentifier(ctx, &phase.ObjectBundle[j])
				instancesStates[j].CurrentReplicaCount = instancesStates[j].DesiredReplicaCount
				ctx.GetState().GetNamespacesState().Set(nsName, id, instancesStates[j])
			}
		}()

	}
	tuningSet.Execute(actions)
	return errList
}

// ExecuteObject executes single test object operation based on provided object configuration.
func (ste *simpleTestExecutor) ExecuteObject(ctx Context, object *api.Object, namespace string, replicaIndex int32, operation OperationType) *errors.ErrorList {
	objName := fmt.Sprintf("%v-%d", object.Basename, replicaIndex)
	var err error
	var obj *unstructured.Unstructured
	switch operation {
	case CREATE_OBJECT, PATCH_OBJECT:
		mapping := ctx.GetTemplateMappingCopy()
		if object.TemplateFillMap != nil {
			util.CopyMap(object.TemplateFillMap, mapping)
		}
		mapping[baseNamePlaceholder] = object.Basename
		mapping[indexPlaceholder] = replicaIndex
		mapping[namePlaceholder] = objName
		mapping[namespacePlaceholder] = namespace
		obj, err = ctx.GetTemplateProvider().TemplateToObject(object.ObjectTemplatePath, mapping)
		if err != nil && err != config.ErrorEmptyFile {
			return errors.NewErrorList(fmt.Errorf("reading template (%v) error: %v", object.ObjectTemplatePath, err))
		}
	case DELETE_OBJECT:
		obj, err = ctx.GetTemplateProvider().RawToObject(object.ObjectTemplatePath)
		if err != nil && err != config.ErrorEmptyFile {
			return errors.NewErrorList(fmt.Errorf("reading template (%v) for deletion error: %v", object.ObjectTemplatePath, err))
		}
	default:
		return errors.NewErrorList(fmt.Errorf("unsupported operation %v for namespace %v object %v", operation, namespace, objName))
	}
	errList := errors.NewErrorList()
	if err == config.ErrorEmptyFile {
		return errList
	}
	gvk := obj.GroupVersionKind()
	switch operation {
	case CREATE_OBJECT:
		if err := ctx.GetClusterFramework().CreateObject(namespace, objName, obj); err != nil {
			errList.Append(fmt.Errorf("namespace %v object %v creation error: %v", namespace, objName, err))
		}
	case PATCH_OBJECT:
		if err := ctx.GetClusterFramework().PatchObject(namespace, objName, obj); err != nil {
			errList.Append(fmt.Errorf("namespace %v object %v updating error: %v", namespace, objName, err))
		}
	case DELETE_OBJECT:
		if err := ctx.GetClusterFramework().DeleteObject(gvk, namespace, objName); err != nil {
			errList.Append(fmt.Errorf("namespace %v object %v deletion error: %v", namespace, objName, err))
		}
	}
	return errList
}

// verifyBundleCorrectness checks if all bundle objects have the same replica count.
func verifyBundleCorrectness(instancesStates []*state.InstancesState) error {
	const uninitialized int32 = -1
	expectedReplicaCount := uninitialized
	for j := range instancesStates {
		if expectedReplicaCount != uninitialized && instancesStates[j].CurrentReplicaCount != expectedReplicaCount {
			return fmt.Errorf("bundle error: %s has %d replicas while %s has %d",
				instancesStates[j].Object.Basename,
				instancesStates[j].CurrentReplicaCount,
				instancesStates[j-1].Object.Basename,
				instancesStates[j-1].CurrentReplicaCount)
		}
		expectedReplicaCount = instancesStates[j].CurrentReplicaCount
	}
	return nil
}

func getIdentifier(ctx Context, object *api.Object) (state.InstancesIdentifier, error) {
	obj, err := ctx.GetTemplateProvider().RawToObject(object.ObjectTemplatePath)
	if err != nil {
		return state.InstancesIdentifier{}, fmt.Errorf("reading template (%v) for identifier error: %v", object.ObjectTemplatePath, err)
	}
	gvk := obj.GroupVersionKind()
	return state.InstancesIdentifier{
		Basename:   object.Basename,
		ObjectKind: gvk.Kind,
		ApiGroup:   gvk.Group,
	}, nil
}

func createNamespacesList(ctx Context, namespaceRange *api.NamespaceRange) []string {
	if namespaceRange == nil {
		// Returns "" which represents cluster level.
		return []string{""}
	}

	nsList := make([]string, 0)
	nsBasename := ctx.GetClusterFramework().GetAutomanagedNamespacePrefix()
	if namespaceRange.Basename != nil {
		nsBasename = *namespaceRange.Basename
	}

	for i := namespaceRange.Min; i <= namespaceRange.Max; i++ {
		nsList = append(nsList, fmt.Sprintf("%v-%d", nsBasename, i))
	}
	return nsList
}

func isErrsCritical(*errors.ErrorList) bool {
	// TODO: define critical errors
	return false
}

func cleanupResources(ctx Context) {
	cleanupStartTime := time.Now()
	ctx.GetMeasurementManager().Dispose()
	if errList := ctx.GetClusterFramework().DeleteAutomanagedNamespaces(); !errList.IsEmpty() {
		klog.Errorf("Resource cleanup error: %v", errList.String())
		return
	}
	klog.Infof("Resources cleanup time: %v", time.Since(cleanupStartTime))
}

func getReplicaCountOfNewObject(ctx Context, namespace string, object *api.Object) (int32, error) {
	if object.ListUnknownObjectOptions == nil {
		return 0, nil
	}
	klog.V(4).Infof("%s: new object detected, will list objects in order to find num replicas", object.Basename)
	selector, err := metav1.LabelSelectorAsSelector(object.ListUnknownObjectOptions.LabelSelector)
	if err != nil {
		return 0, err
	}
	obj, err := ctx.GetTemplateProvider().RawToObject(object.ObjectTemplatePath)
	if err != nil {
		return 0, err
	}
	gvk := obj.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	replicaCount, err := runtimeobjects.GetNumObjectsMatchingSelector(
		ctx.GetClusterFramework().GetDynamicClients().GetClient(),
		namespace,
		gvr,
		selector)
	if err != nil {
		return 0, nil
	}
	klog.V(4).Infof("%s: found %d replicas", object.Basename, replicaCount)
	return int32(replicaCount), nil
}
