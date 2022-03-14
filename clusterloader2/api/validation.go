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

package api

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

// ConfigValidator contains metadata for config validation.
type ConfigValidator struct {
	configDir string
	config    *Config
}

// NewConfigValidator creates a new ConfigValidator object
func NewConfigValidator(configDir string, config *Config) *ConfigValidator {
	return &ConfigValidator{
		configDir: configDir,
		config:    config,
	}
}

// Validate checks and verifies the configuration parameters.
func (v *ConfigValidator) Validate() *errors.ErrorList {
	allErrs := field.ErrorList{}
	c := v.config

	// TODO(#1696): Clean up after removing automanagedNamespaces
	if c.AutomanagedNamespaces < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("automanagedNamespaces"), c.AutomanagedNamespaces, "must be non-negative"))
	}

	allErrs = append(allErrs, v.validateNamespace(&c.Namespace, field.NewPath("namespace"))...)

	for i := range c.TuningSets {
		allErrs = append(allErrs, v.validateTuningSet(c.TuningSets[i], field.NewPath("tuningSets").Index(i))...)
	}

	// validate steps
	if len(c.Steps) == 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("steps"), 0, "cannot be empty"))
	}
	for i := range c.Steps {
		allErrs = append(allErrs, v.validateStep(c.Steps[i], field.NewPath("steps").Index(i))...)
	}
	// Ensure that the tuning set referenced in a phase has been declared
	allErrs = append(allErrs, v.handleTuningSetInPhase()...)

	if len(allErrs) == 0 {
		return nil
	}

	return v.toErrorList(allErrs)
}

func (v *ConfigValidator) toErrorList(fldErrors field.ErrorList) *errors.ErrorList {
	errList := errors.NewErrorList()
	for _, fldError := range fldErrors {
		errList.Append(fldError)
	}
	return errList
}

func (v *ConfigValidator) validateNamespace(ns *NamespaceConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if ns.Number <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("number"), ns.Number, "must be positive"))
	}
	return allErrs
}

func (v *ConfigValidator) validateStep(s *Step, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i := range s.Phases {
		allErrs = append(allErrs, v.validatePhase(s.Phases[i], fldPath.Child("phases").Index(i))...)
	}
	for i := range s.Measurements {
		allErrs = append(allErrs, v.validateMeasurement(s.Measurements[i], fldPath.Child("measurements").Index(i))...)
	}
	isPhasesEmpty := len(s.Phases) == 0
	isMeasurementsEmpty := len(s.Measurements) == 0
	if isPhasesEmpty && isMeasurementsEmpty {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("measurements"), s.Measurements, "can't be empty with empty phases"))
	}
	if !isPhasesEmpty && !isMeasurementsEmpty {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("measurements"), s.Measurements, "can't be non-empty with non-empty phases"))
	}
	return allErrs
}

func (v *ConfigValidator) handleTuningSetInPhase() field.ErrorList {
	allErrs := field.ErrorList{}
	c := v.config
	for i := range c.Steps {
		allErrs = append(allErrs, validateTuningSetInPhase(c.Steps[i], c.TuningSets, field.NewPath("steps").Index(i))...)
	}
	return allErrs
}

func validateTuningSetInPhase(s *Step, ts []*TuningSet, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	for i := range s.Phases {
		if s.Phases[i].TuningSet != "" {
			found := false
			for j := range ts {
				if s.Phases[i].TuningSet == ts[j].Name {
					found = true
					break
				}
			}
			if found == false {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("phases").Index(i).Child("tuningSet"), s.Phases[i].TuningSet, "tuning set referenced has not been declared"))
			}
		}
	}
	return allErrs
}

func (v *ConfigValidator) validatePhase(p *Phase, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if p.NamespaceRange != nil {
		allErrs = append(allErrs, v.validateNamespaceRange(p.NamespaceRange, fldPath.Child("namespaceRange"))...)
	}
	if p.ReplicasPerNamespace < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("replicasPerNamespace"), p.ReplicasPerNamespace, "must be non-negative"))
	}
	for i := range p.ObjectBundle {
		allErrs = append(allErrs, v.validateObject(p.ObjectBundle[i], fldPath.Child("objectBundle").Index(i))...)
	}
	return allErrs
}

func (v *ConfigValidator) validateObject(o *Object, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	errs := validation.IsDNS1123Subdomain(o.Basename)
	for i := range errs {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("basename"), o.Basename, errs[i]))
	}
	if !v.fileExists(o.ObjectTemplatePath) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("objectTemplatePath"), o.ObjectTemplatePath, "file must exist"))
	}
	return allErrs
}

func (v *ConfigValidator) validateNamespaceRange(nr *NamespaceRange, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if nr.Min < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("min"), nr.Min, "must be non-negative"))
	}
	if nr.Max < nr.Min {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("max"), nr.Max, "must be greater than min"))
	}
	return allErrs
}

func (v *ConfigValidator) validateTuningSet(ts *TuningSet, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	tuningSetsNumber := 0
	if ts.QPSLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateQPSLoad(ts.QPSLoad, fldPath.Child("qpsLoad"))...)
	}
	if ts.RandomizedLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateRandomizedLoad(ts.RandomizedLoad, fldPath.Child("randomizedLoad"))...)
	}
	if ts.SteppedLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateSteppedLoad(ts.SteppedLoad, fldPath.Child("steppedLoad"))...)
	}
	if ts.TimeLimitedLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateTimeLimitedLoad(ts.TimeLimitedLoad, fldPath.Child("timeLimitedLoad"))...)
	}
	if ts.RandomizedTimeLimitedLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateRandomizedTimeLimitedLoad(ts.RandomizedTimeLimitedLoad, fldPath.Child("randomizedTimeLimitedLoad"))...)
	}
	if ts.ParallelismLimitedLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateParallelismTimeLimitedLoad(ts.ParallelismLimitedLoad, fldPath.Child("parallelismLimitedLoad"))...)
	}
	if ts.GlobalQPSLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, v.validateGlobalQPSLoad(ts.GlobalQPSLoad, fldPath.Child("globalQPSLoad"))...)
	}
	if tuningSetsNumber != 1 {
		allErrs = append(allErrs, field.Forbidden(fldPath, "must specify exactly 1 tuning set type"))
	}

	return allErrs
}

func (v *ConfigValidator) validateMeasurement(m *Measurement, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(m.Instances) != 0 && m.Identifier != "" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("Identifier"), m.Identifier, " cannot be non empty when Instances specified"))
	}
	if len(m.Instances) == 0 && m.Identifier == "" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("Identifier"), m.Identifier, " and Instances cannot be both empty"))
	}
	return allErrs
}

func (v *ConfigValidator) validateQPSLoad(ql *QPSLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ql.QPS <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("qps"), ql.QPS, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateRandomizedLoad(rl *RandomizedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if rl.AverageQPS <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("averageQps"), rl.AverageQPS, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateSteppedLoad(sl *SteppedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if sl.BurstSize <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("burstSize"), sl.BurstSize, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateTimeLimitedLoad(tll *TimeLimitedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if tll.TimeLimit <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("timeLimit"), tll.TimeLimit, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateRandomizedTimeLimitedLoad(rtl *RandomizedTimeLimitedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if rtl.TimeLimit <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("timeLimit"), rtl.TimeLimit, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateParallelismTimeLimitedLoad(ptl *ParallelismLimitedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ptl.ParallelismLimit <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("parallelismLimit"), ptl.ParallelismLimit, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) validateGlobalQPSLoad(gl *GlobalQPSLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if gl.QPS <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("qps"), gl.QPS, "must have positive value"))
	}
	if gl.Burst <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("burst"), gl.Burst, "must have positive value"))
	}
	return allErrs
}

func (v *ConfigValidator) fileExists(path string) bool {
	cwd, _ := os.Getwd()
	_, err := os.Stat(fmt.Sprintf("%s/%s/%s", cwd, v.configDir, path))
	if os.IsNotExist(err) {
		// If relative path didn't work, we also try absolute path.
		_, err = os.Stat(fmt.Sprintf("%s/%s", v.configDir, path))
	}
	return !os.IsNotExist(err)
}
