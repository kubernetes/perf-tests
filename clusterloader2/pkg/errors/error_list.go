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

package errors

import (
	"bytes"
	"sync"
)

// ErrorList is a thread-safe error list.
type ErrorList struct {
	lock   sync.Mutex
	errors []error
}

// NewErrorList creates new error list.
func NewErrorList(errors ...error) *ErrorList {
	return &ErrorList{
		errors: errors,
	}
}

// IsEmpty returns true if there is no error in the list.
func (e *ErrorList) IsEmpty() bool {
	e.lock.Lock()
	defer e.lock.Unlock()
	return len(e.errors) == 0
}

// Append adds errors to the list
func (e *ErrorList) Append(errs ...error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.errors = append(e.errors, errs...)
}

// Concat concatenates error lists.
func (e *ErrorList) Concat(e2 *ErrorList) {
	if e2 == nil {
		return
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	e.errors = append(e.errors, e2.errors...)
}

// String returns error list as a single string.
func (e *ErrorList) String() string {
	e.lock.Lock()
	defer e.lock.Unlock()
	var b bytes.Buffer
	b.WriteString("[")
	for i := 0; i < len(e.errors); i++ {
		b.WriteString(e.errors[i].Error())
		if i != len(e.errors)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("]")
	return b.String()
}

// Error returns string representation of ErrorList.
func (e *ErrorList) Error() string {
	return e.String()
}
