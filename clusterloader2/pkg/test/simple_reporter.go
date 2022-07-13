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
	"time"

	ginkgoconfig "github.com/onsi/ginkgo/config"
	ginkgoreporters "github.com/onsi/ginkgo/reporters"
	ginkgotypes "github.com/onsi/ginkgo/types"
	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

type simpleReporter struct {
	testName       string
	suiteStart     time.Time
	junitReporter  *ginkgoreporters.JUnitReporter
	suiteSummary   *ginkgotypes.SuiteSummary
	stepsSummaries []*ginkgotypes.SpecSummary
}

func CreateSimpleReporter(reportFilename, testSuiteDescription string) Reporter {
	return &simpleReporter{
		junitReporter: ginkgoreporters.NewJUnitReporter(reportFilename),
		suiteSummary: &ginkgotypes.SuiteSummary{
			SuiteDescription: testSuiteDescription,
		},
	}
}

func (str *simpleReporter) SetTestName(name string) {
	str.testName = name
}

func (str *simpleReporter) GetNumberOfFailedTestItems() int {
	return str.suiteSummary.NumberOfFailedSpecs
}

func (str *simpleReporter) BeginTestSuite() {
	str.junitReporter.SpecSuiteWillBegin(ginkgoconfig.GinkgoConfig, str.suiteSummary)
	str.suiteStart = time.Now()
}

func (str *simpleReporter) EndTestSuite() {
	str.suiteSummary.RunTime = time.Since(str.suiteStart)
	str.junitReporter.SpecSuiteDidEnd(str.suiteSummary)
}

func (str *simpleReporter) ReportTestStepFinish(duration time.Duration, stepName string, errList *errors.ErrorList) {
	stepSummary := &ginkgotypes.SpecSummary{
		ComponentTexts: []string{str.suiteSummary.SuiteDescription, fmt.Sprintf("%s: %s", str.testName, stepName)},
		RunTime:        duration,
	}
	str.handleSummary(stepSummary, errList)
	str.stepsSummaries = append(str.stepsSummaries, stepSummary)
}

func (str *simpleReporter) ReportTestStep(result *StepResult) {
	for _, subtestResult := range result.getAllResults() {
		str.ReportTestStepFinish(subtestResult.duration, subtestResult.name, subtestResult.err)
	}
}

func (str *simpleReporter) ReportTestFinish(duration time.Duration, testConfigPath string, errList *errors.ErrorList) {
	testSummary := &ginkgotypes.SpecSummary{
		ComponentTexts: []string{str.suiteSummary.SuiteDescription, fmt.Sprintf("%s overall (%s)", str.testName, testConfigPath)},
		RunTime:        duration,
	}
	str.handleSummary(testSummary, errList)

	str.junitReporter.SpecDidComplete(testSummary)
	for _, stepSummary := range str.stepsSummaries {
		str.junitReporter.SpecDidComplete(stepSummary)
	}
	str.stepsSummaries = nil
}

func (str *simpleReporter) handleSummary(summary *ginkgotypes.SpecSummary, errList *errors.ErrorList) {
	if errList.IsEmpty() {
		summary.State = ginkgotypes.SpecStatePassed
	} else {
		summary.State = ginkgotypes.SpecStateFailed
		summary.Failure = ginkgotypes.SpecFailure{
			Message: errList.String(),
		}
		str.suiteSummary.NumberOfFailedSpecs++
	}
}
