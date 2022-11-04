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

package test

import (
	"fmt"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

// flattenModuleSteps computes a slice of executable steps. An executable step
// is either a measurement or phase step. The method recursively flattens the
// steps being part of a module while skipping the module steps itself. E.g. if
// the input (after loading the modules from their files) is:
// - step 1:
// - step 2:
//   module:
//     - step 3:
//     - step 4:
//       module:
//         -step 5
// - step 6
// the method will return [step1, step3, step5, step6].
func flattenModuleSteps(ctx Context, unprocessedSteps []*api.Step) ([]*api.Step, error) {
	var processedSteps []*api.Step
	for i := range unprocessedSteps {
		step := unprocessedSteps[i]
		if step.IsModule() {
			module, err := loadModule(ctx, &step.Module)
			if err != nil {
				return nil, err
			}
			processedModuleSteps, err := flattenModuleSteps(ctx, module.Steps)
			if err != nil {
				return nil, err
			}
			processedSteps = append(processedSteps, processedModuleSteps...)
		} else {
			processedSteps = append(processedSteps, step)
		}
	}
	return processedSteps, nil
}

// loadModule loads the module pointed by the moduleRef.
func loadModule(ctx Context, moduleRef *api.ModuleRef) (*api.Module, error) {
	if moduleRef.Path == "" {
		return nil, fmt.Errorf("path not set: %#v", moduleRef)
	}
	mapping := ctx.GetTemplateMappingCopy()
	if moduleRef.Params != nil {
		util.CopyMap(moduleRef.Params, mapping)
	}
	var module api.Module
	if err := ctx.GetTemplateProvider().TemplateInto(moduleRef.Path, mapping, &module); err != nil {
		return nil, fmt.Errorf("error while processing module %#v: %w", moduleRef, err)
	}
	return &module, nil
}
