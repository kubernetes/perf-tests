/*
Copyright 2022 The Kubernetes Authors.

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
	"sort"
	"sync"
	"time"

	"k8s.io/perf-tests/clusterloader2/pkg/errors"
)

type SubstepResult struct {
	name     string
	id       int
	duration time.Duration
	err      *errors.ErrorList
}

func (ss *SubstepResult) Name() string                    { return ss.name }
func (ss *SubstepResult) Duration() time.Duration         { return ss.duration }
func (ss *SubstepResult) GetAllErrors() *errors.ErrorList { return ss.err }

type StepResult struct {
	lock      sync.Mutex
	startTime time.Time
	name      string
	err       *errors.ErrorList

	results []SubstepResult
}

func NewStepResult(stepName string) *StepResult {
	return &StepResult{
		name:      stepName,
		startTime: time.Now(),
		results:   []SubstepResult{},
		err:       errors.NewErrorList(),
	}
}

func (s *StepResult) Name() string { return s.name }

func (s *StepResult) AddSubStepResult(name string, id int, err *errors.ErrorList) {
	s.lock.Lock()
	defer s.lock.Unlock()

	duration := time.Since(s.startTime)
	s.results = append(s.results, SubstepResult{
		name:     name,
		id:       id,
		duration: duration,
		err:      err,
	})
}

func (s *StepResult) getAllErrorsUnsafe() *errors.ErrorList {
	errList := errors.NewErrorList()
	errList.Concat(s.err)
	for _, value := range s.results {
		errList.Concat(value.err)
	}
	return errList
}

func (s *StepResult) GetAllErrors() *errors.ErrorList {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.getAllErrorsUnsafe()
}

func (s *StepResult) GetAllResults() []SubstepResult {
	s.lock.Lock()
	defer s.lock.Unlock()

	results := []SubstepResult{}
	// Special case for phases that do not report substeps.
	if len(s.results) == 0 {
		results = append(results, SubstepResult{
			name:     s.name,
			duration: time.Since(s.startTime),
			err:      s.getAllErrorsUnsafe(),
		})
	}

	sort.Slice(s.results, func(i, j int) bool {
		return s.results[i].id < s.results[j].id
	})

	for _, result := range s.results {
		results = append(results, SubstepResult{
			name:     s.name + " " + result.name,
			duration: result.duration,
			err:      result.err,
		})
	}

	return results
}

func (s *StepResult) AddStepError(errs *errors.ErrorList) {
	s.err.Concat(errs)
}
