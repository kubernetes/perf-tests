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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEnvVar = "CL2_FLAGS_TEST_ENV_VAR"

func TestParseEnvString(t *testing.T) {
	testCases := []struct {
		name     string
		envVar   string
		setEnv   bool
		envValue string
		want     string
	}{
		{name: "unset uses default", envVar: testEnvVar, want: "default"},
		{name: "set overrides default", envVar: testEnvVar, setEnv: true, envValue: "custom", want: "custom"},
		{name: "set to empty overrides default", envVar: testEnvVar, setEnv: true, envValue: "", want: ""},
		{name: "no variable name uses default", envVar: "", setEnv: true, envValue: "custom", want: "default"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(testEnvVar, tc.envValue)
			}
			var got string
			require.NoError(t, parseEnvString(&got, tc.envVar, "default"))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseEnvStringSlice(t *testing.T) {
	defaultValue := []string{"x", "y"}
	testCases := []struct {
		name     string
		envVar   string
		setEnv   bool
		envValue string
		want     []string
	}{
		{name: "unset uses default", envVar: testEnvVar, want: defaultValue},
		{name: "single value", envVar: testEnvVar, setEnv: true, envValue: "a", want: []string{"a"}},
		{name: "multiple values", envVar: testEnvVar, setEnv: true, envValue: "a,b", want: []string{"a", "b"}},
		{name: "values are not trimmed", envVar: testEnvVar, setEnv: true, envValue: "a, b", want: []string{"a", " b"}},
		{name: "set to empty uses default", envVar: testEnvVar, setEnv: true, envValue: "", want: defaultValue},
		{name: "no variable name uses default", envVar: "", setEnv: true, envValue: "a,b", want: defaultValue},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(testEnvVar, tc.envValue)
			}
			var got []string
			require.NoError(t, parseEnvStringSlice(&got, tc.envVar, defaultValue))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseEnvInt(t *testing.T) {
	testCases := []struct {
		name     string
		envVar   string
		setEnv   bool
		envValue string
		want     int
		wantErr  bool
	}{
		{name: "unset uses default", envVar: testEnvVar, want: 3},
		{name: "valid value", envVar: testEnvVar, setEnv: true, envValue: "42", want: 42},
		{name: "negative value", envVar: testEnvVar, setEnv: true, envValue: "-1", want: -1},
		{name: "invalid value keeps default", envVar: testEnvVar, setEnv: true, envValue: "abc", want: 3, wantErr: true},
		{name: "empty value keeps default", envVar: testEnvVar, setEnv: true, envValue: "", want: 3, wantErr: true},
		{name: "no variable name uses default", envVar: "", setEnv: true, envValue: "42", want: 3},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(testEnvVar, tc.envValue)
			}
			var got int
			err := parseEnvInt(&got, tc.envVar, 3)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseEnvBool(t *testing.T) {
	testCases := []struct {
		name     string
		envVar   string
		setEnv   bool
		envValue string
		want     bool
		wantErr  bool
	}{
		{name: "unset uses default", envVar: testEnvVar, want: false},
		{name: "true", envVar: testEnvVar, setEnv: true, envValue: "true", want: true},
		{name: "one", envVar: testEnvVar, setEnv: true, envValue: "1", want: true},
		{name: "invalid value keeps default", envVar: testEnvVar, setEnv: true, envValue: "yes", want: false, wantErr: true},
		{name: "empty value keeps default", envVar: testEnvVar, setEnv: true, envValue: "", want: false, wantErr: true},
		{name: "no variable name uses default", envVar: "", setEnv: true, envValue: "true", want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(testEnvVar, tc.envValue)
			}
			var got bool
			err := parseEnvBool(&got, tc.envVar, false)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseEnvDuration(t *testing.T) {
	testCases := []struct {
		name     string
		envVar   string
		setEnv   bool
		envValue string
		want     time.Duration
		wantErr  bool
	}{
		{name: "unset uses default", envVar: testEnvVar, want: time.Minute},
		{name: "valid value", envVar: testEnvVar, setEnv: true, envValue: "5s", want: 5 * time.Second},
		{name: "compound value", envVar: testEnvVar, setEnv: true, envValue: "1h30m", want: 90 * time.Minute},
		{name: "missing unit keeps default", envVar: testEnvVar, setEnv: true, envValue: "5", want: time.Minute, wantErr: true},
		{name: "empty value keeps default", envVar: testEnvVar, setEnv: true, envValue: "", want: time.Minute, wantErr: true},
		{name: "no variable name uses default", envVar: "", setEnv: true, envValue: "5s", want: time.Minute},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv(testEnvVar, tc.envValue)
			}
			var got time.Duration
			err := parseEnvDuration(&got, tc.envVar, time.Minute)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
