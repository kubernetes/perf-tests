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

package flags

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

var _ pflag.Value = (*stringFlagFunc)(nil)
var _ flagFunc = (*stringFlagFunc)(nil)
var _ pflag.Value = (*intFlagFunc)(nil)
var _ flagFunc = (*intFlagFunc)(nil)

type flagFunc interface {
	initialize() error
}

type stringFlagFunc struct {
	valPtr         *string
	initializeFunc func() error
}

// initialize runs additional parsing function.
func (s *stringFlagFunc) initialize() error {
	return s.initializeFunc()
}

// String returns default string.
func (s *stringFlagFunc) String() string {
	return ""
}

// Set handles flag value setting.
func (s *stringFlagFunc) Set(val string) error {
	*s.valPtr = val
	return nil
}

// Type returns flag type.
func (s *stringFlagFunc) Type() string {
	return "string"
}

type stringSliceFlagFunc struct {
	valPtr         *[]string
	initializeFunc func() error
}

// initialize runs additional parsing function.
func (s *stringSliceFlagFunc) initialize() error {
	return s.initializeFunc()
}

// String returns default string.
func (*stringSliceFlagFunc) String() string {
	return "false"
}

// Set handles flag value setting.
func (s *stringSliceFlagFunc) Set(val string) error {
	if val != "" {
		*s.valPtr = strings.Split(val, ",")
	} else {
		*s.valPtr = nil
	}
	return nil
}

// Type returns flag type.
func (*stringSliceFlagFunc) Type() string {
	return "stringSlice"
}

type intFlagFunc struct {
	valPtr         *int
	initializeFunc func() error
}

// initialize runs additional parsing function.
func (i *intFlagFunc) initialize() error {
	return i.initializeFunc()
}

// String returns default string.
func (i *intFlagFunc) String() string {
	return "0"
}

// Set handles flag value setting.
func (i *intFlagFunc) Set(val string) error {
	iVal, err := strconv.Atoi(val)
	if err != nil {
		return err
	}
	*i.valPtr = iVal
	return nil
}

// Type returns flag type.
func (i *intFlagFunc) Type() string {
	return "int"
}

type boolFlagFunc struct {
	valPtr         *bool
	initializeFunc func() error
}

// initialize runs additional parsing function.
func (b *boolFlagFunc) initialize() error {
	return b.initializeFunc()
}

// String returns default string.
func (*boolFlagFunc) String() string {
	return "false"
}

// Set handles flag value setting.
func (b *boolFlagFunc) Set(val string) error {
	bVal, err := strconv.ParseBool(val)
	if err != nil {
		return err
	}
	*b.valPtr = bVal
	return nil
}

// Type returns flag type.
func (*boolFlagFunc) Type() string {
	return "bool"
}

type durationFlagFunc struct {
	valPtr         *time.Duration
	initializeFunc func() error
}

// initialize runs additional parsing function.
func (d *durationFlagFunc) initialize() error {
	return d.initializeFunc()
}

// String returns default string.
func (*durationFlagFunc) String() string {
	return "0s"
}

// Set handles flag value setting.
func (d *durationFlagFunc) Set(val string) error {
	dVal, err := time.ParseDuration(val)
	if err != nil {
		return err
	}
	*d.valPtr = dVal
	return nil
}

// Type returns flag type.
func (*durationFlagFunc) Type() string {
	return "time.Duration"
}
