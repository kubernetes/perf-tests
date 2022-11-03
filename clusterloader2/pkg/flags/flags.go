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
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

func init() {
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
}

var flags []flagFunc

// StringVar creates string flag with given parameters.
func StringVar(s *string, flagName, defaultValue, description string) {
	pflag.StringVar(s, flagName, defaultValue, description)
}

// IntVar creates int flag with given parameters.
func IntVar(i *int, flagName string, defaultValue int, description string) {
	pflag.IntVar(i, flagName, defaultValue, description)
}

// BoolVar creates a bool flag with given parameters.
func BoolVar(b *bool, flagName string, defaultValue bool, description string) {
	pflag.BoolVar(b, flagName, defaultValue, description)
}

// DurationVar creates a time.Duration flag with given parameters.
func DurationVar(d *time.Duration, flagName string, defaultValue time.Duration, description string) {
	pflag.DurationVar(d, flagName, defaultValue, description)
}

// StringEnvVar creates string flag with given parameters.
// If flag is not provided, it will try to get env variable.
func StringEnvVar(s *string, flagName, envVariable, defaultValue, description string) {
	stringFlag := &stringFlagFunc{
		valPtr:         s,
		initializeFunc: func() error { return parseEnvString(s, envVariable, defaultValue) },
	}
	pflag.Var(stringFlag, flagName, description)
	flags = append(flags, stringFlag)
}

// StringArrayVar creates string flag with given parameters. Flag can be used multiple times.
func StringArrayVar(s *[]string, flagName string, defaultValue []string, description string) {
	pflag.StringArrayVar(s, flagName, defaultValue, description)
}

// StringSliceEnvVar creates a string slice flag with the given parameters.
// If the flag is not provided, it will try to get the env variable.
// Flag accepts multiple values separated by commas.
func StringSliceEnvVar(s *[]string, flagName, envVariable string, defaultValue []string, description string) {
	stringSliceFlag := &stringSliceFlagFunc{
		valPtr:         s,
		initializeFunc: func() error { return parseEnvStringSlice(s, envVariable, defaultValue) },
	}
	pflag.Var(stringSliceFlag, flagName, description)
	flags = append(flags, stringSliceFlag)
}

// IntEnvVar creates int flag with given parameters.
// If flag is not provided, it will try to get env variable.
func IntEnvVar(i *int, flagName, envVariable string, defaultValue int, description string) {
	intFlag := &intFlagFunc{
		valPtr:         i,
		initializeFunc: func() error { return parseEnvInt(i, envVariable, defaultValue) },
	}
	pflag.Var(intFlag, flagName, description)
	flags = append(flags, intFlag)
}

// BoolEnvVar creates bool flag with given parameters.
// If flag is not provided, it will try to get env variable.
func BoolEnvVar(b *bool, flagName, envVariable string, defaultValue bool, description string) {
	boolFlag := &boolFlagFunc{
		valPtr:         b,
		initializeFunc: func() error { return parseEnvBool(b, envVariable, defaultValue) },
	}
	// Set NoOptDefValue, to make --flag-name equivalent to --flag-name=true
	pflag.CommandLine.VarPF(boolFlag, flagName, "", description).NoOptDefVal = "true"
	flags = append(flags, boolFlag)
}

// DurationEnvVar creates time.Duration flag with given parameters.
// If flag is not provided, it will try to get env variable.
func DurationEnvVar(d *time.Duration, flagName, envVariable string, defaultValue time.Duration, description string) {
	durationFlag := &durationFlagFunc{
		valPtr:         d,
		initializeFunc: func() error { return parseEnvDuration(d, envVariable, defaultValue) },
	}
	pflag.Var(durationFlag, flagName, description)
	flags = append(flags, durationFlag)
}

// Parse parses provided flags and env variables.
func Parse() error {
	for i := range flags {
		if err := flags[i].initialize(); err != nil {
			return err
		}
	}
	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		return err
	}
	return nil
}

// MarkDeprecated indicates that a flag is deprecated
func MarkDeprecated(name string, usageMessage string) error {
	return pflag.CommandLine.MarkDeprecated(name, usageMessage)
}

func parseEnvString(s *string, envVariable, defaultValue string) error {
	*s = defaultValue
	if envVariable != "" {
		if val, ok := os.LookupEnv(envVariable); ok {
			*s = val
			return nil
		}
	}
	return nil
}

func parseEnvStringSlice(s *[]string, envVariable string, defaultValue []string) error {
	*s = defaultValue
	if envVariable != "" {
		if val, ok := os.LookupEnv(envVariable); ok && val != "" {
			*s = strings.Split(val, ",")
		}
	}
	return nil
}

func parseEnvInt(i *int, envVariable string, defaultValue int) error {
	*i = defaultValue
	if envVariable != "" {
		if val, ok := os.LookupEnv(envVariable); ok {
			iVal, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("parsing env variable %s failed", envVariable)
			}
			*i = iVal
			return nil
		}
	}
	return nil
}

func parseEnvBool(b *bool, envVariable string, defaultValue bool) error {
	*b = defaultValue
	if envVariable != "" {
		if val, ok := os.LookupEnv(envVariable); ok {
			bVal, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("parsing env variable %s failed", envVariable)
			}
			*b = bVal
			return nil
		}
	}
	return nil
}

func parseEnvDuration(d *time.Duration, envVariable string, defaultValue time.Duration) error {
	*d = defaultValue
	if envVariable != "" {
		if val, ok := os.LookupEnv(envVariable); ok {
			dVal, err := time.ParseDuration(val)
			if err != nil {
				return fmt.Errorf("parsing env variable %s failed", envVariable)
			}
			*d = dVal
			return nil
		}
	}
	return nil
}
