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

package validation

import (
	"os"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/perf-tests/clusterloader2/api"
)

// VerifyConfig does a simple validation for config and all included structures.
// Returns list of all not satisfied conditions.
func VerifyConfig(c *api.Config) field.ErrorList {
	allErrs := field.ErrorList{}
	if c.AutomanagedNamespaces < 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("automanagedNamespaces"), c.AutomanagedNamespaces, "must be non-negative"))
	}
	for i := range c.TuningSets {
		allErrs = append(allErrs, verifyTuningSet(&c.TuningSets[i], field.NewPath("tuningSets").Index(i))...)
	}
	for i := range c.Steps {
		allErrs = append(allErrs, verifyStep(&c.Steps[i], field.NewPath("steps").Index(i))...)
	}
	return allErrs
}

func verifyStep(s *api.Step, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i := range s.Phases {
		allErrs = append(allErrs, verifyPhase(&s.Phases[i], fldPath.Child("phases").Index(i))...)
	}
	for i := range s.Measurements {
		allErrs = append(allErrs, verifyMeasurement(&s.Measurements[i], fldPath.Child("measurements").Index(i))...)
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

func verifyPhase(p *api.Phase, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if p.NamespaceRange != nil {
		allErrs = append(allErrs, verifyNamespaceRange(p.NamespaceRange, fldPath.Child("namespaceRange"))...)
	}
	if p.ReplicasPerNamespace < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("replicasPerNamespace"), p.ReplicasPerNamespace, "must be non-negative"))
	}
	for i := range p.ObjectBundle {
		allErrs = append(allErrs, verifyObject(&p.ObjectBundle[i], fldPath.Child("objectBundle").Index(i))...)
	}
	return allErrs
}

func verifyObject(o *api.Object, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	errs := validation.IsDNS1123Subdomain(o.Basename)
	for i := range errs {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("basename"), o.Basename, errs[i]))
	}
	if !fileExists(o.ObjectTemplatePath) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("objectTemplatePath"), o.ObjectTemplatePath, "file must exists"))
	}
	return allErrs
}

func verifyNamespaceRange(nr *api.NamespaceRange, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if nr.Min < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("min"), nr.Min, "must be non-negative"))
	}
	if nr.Max < nr.Min {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("max"), nr.Max, "must be greater than min"))
	}
	return allErrs
}

func verifyTuningSet(ts *api.TuningSet, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	tuningSetsNumber := 0
	if ts.QpsLoad != nil {
		tuningSetsNumber++
		allErrs = append(allErrs, verifyQpsLoad(ts.QpsLoad, fldPath.Child("qpsLoad"))...)
	}
	if ts.RandomizedLoad != nil {
		if tuningSetsNumber > 0 {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("randomizedLoad"), "may not specify more than 1 tuning set type"))
		} else {
			tuningSetsNumber++
			allErrs = append(allErrs, verifyRandomizedLoad(ts.RandomizedLoad, fldPath.Child("randomizedLoad"))...)
		}
	}
	if ts.SteppedLoad != nil {
		if tuningSetsNumber > 0 {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("steppedLoad"), "may not specify more than 1 tuning set type"))
		} else {
			tuningSetsNumber++
			allErrs = append(allErrs, verifySteppedLoad(ts.SteppedLoad, fldPath.Child("steppedLoad"))...)
		}
	}
	return allErrs
}

func verifyMeasurement(_ *api.Measurement, _ *field.Path) field.ErrorList {
	return nil
}

func verifyQpsLoad(ql *api.QpsLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ql.Qps <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("qps"), ql.Qps, "must have positive value"))
	}
	return allErrs
}

func verifyRandomizedLoad(rl *api.RandomizedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if rl.AverageQps <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("averageQps"), rl.AverageQps, "must have positive value"))
	}
	return allErrs
}

func verifySteppedLoad(sl *api.SteppedLoad, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if sl.BurstSize <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("burstSize"), sl.BurstSize, "must have positive value"))
	}
	return allErrs
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
