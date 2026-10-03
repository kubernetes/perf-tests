/*
Copyright The Kubernetes Authors.

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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringFlagFuncSet(t *testing.T) {
	val := "previous"
	f := &stringFlagFunc{valPtr: &val}

	require.NoError(t, f.Set("hello"))
	assert.Equal(t, "hello", val)

	require.NoError(t, f.Set(""))
	assert.Equal(t, "", val)
}

func TestStringSliceFlagFuncSet(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "single value", input: "a", want: []string{"a"}},
		{name: "multiple values", input: "a,b,c", want: []string{"a", "b", "c"}},
		{name: "values are not trimmed", input: "a, b", want: []string{"a", " b"}},
		{name: "empty string clears the slice", input: "", want: nil},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val := []string{"previous"}
			f := &stringSliceFlagFunc{valPtr: &val}
			require.NoError(t, f.Set(tc.input))
			assert.Equal(t, tc.want, val)
		})
	}
}

func TestIntFlagFuncSet(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "positive", input: "42", want: 42},
		{name: "negative", input: "-7", want: -7},
		{name: "zero", input: "0", want: 0},
		{name: "not a number keeps previous value", input: "abc", want: 5, wantErr: true},
		{name: "decimal keeps previous value", input: "1.5", want: 5, wantErr: true},
		{name: "empty keeps previous value", input: "", want: 5, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val := 5
			f := &intFlagFunc{valPtr: &val}
			err := f.Set(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, val)
		})
	}
}

func TestBoolFlagFuncSet(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{name: "false", input: "false", want: false},
		{name: "true", input: "true", want: true},
		{name: "zero", input: "0", want: false},
		{name: "uppercase", input: "FALSE", want: false},
		{name: "yes is invalid and keeps previous value", input: "yes", want: true, wantErr: true},
		{name: "empty keeps previous value", input: "", want: true, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val := true
			f := &boolFlagFunc{valPtr: &val}
			err := f.Set(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, val)
		})
	}
}

func TestDurationFlagFuncSet(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "seconds", input: "5s", want: 5 * time.Second},
		{name: "compound", input: "1m30s", want: 90 * time.Second},
		{name: "milliseconds", input: "100ms", want: 100 * time.Millisecond},
		{name: "zero without unit", input: "0", want: 0},
		{name: "missing unit keeps previous value", input: "5", want: time.Hour, wantErr: true},
		{name: "not a duration keeps previous value", input: "abc", want: time.Hour, wantErr: true},
		{name: "empty keeps previous value", input: "", want: time.Hour, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val := time.Hour
			f := &durationFlagFunc{valPtr: &val}
			err := f.Set(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, val)
		})
	}
}

func TestFlagFuncTypes(t *testing.T) {
	assert.Equal(t, "string", (&stringFlagFunc{}).Type())
	assert.Equal(t, "stringSlice", (&stringSliceFlagFunc{}).Type())
	assert.Equal(t, "int", (&intFlagFunc{}).Type())
	assert.Equal(t, "bool", (&boolFlagFunc{}).Type())
	assert.Equal(t, "time.Duration", (&durationFlagFunc{}).Type())
}

func TestFlagFuncDefaultStrings(t *testing.T) {
	assert.Equal(t, "", (&stringFlagFunc{}).String())
	assert.Equal(t, "0", (&intFlagFunc{}).String())
	assert.Equal(t, "false", (&boolFlagFunc{}).String())
	assert.Equal(t, "0s", (&durationFlagFunc{}).String())
}

func TestFlagFuncInitialize(t *testing.T) {
	wantErr := errors.New("initialize failed")
	calls := 0
	initFunc := func() error {
		calls++
		return wantErr
	}
	flagFuncs := []flagFunc{
		&stringFlagFunc{initializeFunc: initFunc},
		&stringSliceFlagFunc{initializeFunc: initFunc},
		&intFlagFunc{initializeFunc: initFunc},
		&boolFlagFunc{initializeFunc: initFunc},
		&durationFlagFunc{initializeFunc: initFunc},
	}
	for _, f := range flagFuncs {
		assert.ErrorIs(t, f.initialize(), wantErr)
	}
	assert.Equal(t, len(flagFuncs), calls)
}
