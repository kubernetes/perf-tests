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

package prom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
)

func ptrString(s string) *string {
	return &s
}

func TestResolveGCPMonitoringEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string defaults to prod endpoint",
			input:    "",
			expected: "https://monitoring.googleapis.com",
		},
		{
			name:     "whitespace string defaults to prod endpoint",
			input:    "   ",
			expected: "https://monitoring.googleapis.com",
		},
		{
			name:     "https scheme is preserved",
			input:    "https://staging-monitoring.sandbox.googleapis.com",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "http scheme is preserved",
			input:    "http://staging-monitoring.sandbox.googleapis.com",
			expected: "http://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "scheme is added if missing",
			input:    "staging-monitoring.sandbox.googleapis.com",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "single trailing slash is stripped",
			input:    "https://staging-monitoring.sandbox.googleapis.com/",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "multiple trailing slashes are stripped",
			input:    "https://staging-monitoring.sandbox.googleapis.com///",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "trailing slash stripped when scheme is missing",
			input:    "staging-monitoring.sandbox.googleapis.com/",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
		{
			name:     "whitespace and trailing slashes stripped",
			input:    "   https://staging-monitoring.sandbox.googleapis.com/   ",
			expected: "https://staging-monitoring.sandbox.googleapis.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveGCPMonitoringEndpoint(tc.input)
			if got != tc.expected {
				t.Errorf("resolveGCPMonitoringEndpoint(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestNewGCPManagedPrometheusClient_MissingProject(t *testing.T) {
	t.Run("PROJECT unset", func(t *testing.T) {
		origProject, isSet := os.LookupEnv(projectEnv)
		if isSet {
			os.Unsetenv(projectEnv)
			t.Cleanup(func() {
				os.Setenv(projectEnv, origProject)
			})
		}

		client, err := NewGCPManagedPrometheusClient()
		if err == nil {
			t.Fatal("expected error when PROJECT is unset, got nil")
		}
		if client != nil {
			t.Fatalf("expected nil client when PROJECT is unset, got %v", client)
		}
		expectedErrMsg := "PROJECT environment variable must be set for GCP managed prometheus client"
		if err.Error() != expectedErrMsg {
			t.Errorf("expected error message %q, got %q", expectedErrMsg, err.Error())
		}
	})

	t.Run("PROJECT empty string", func(t *testing.T) {
		t.Setenv(projectEnv, "")
		client, err := NewGCPManagedPrometheusClient()
		if err == nil {
			t.Fatal("expected error when PROJECT is empty string, got nil")
		}
		if client != nil {
			t.Fatalf("expected nil client when PROJECT is empty, got %v", client)
		}
		expectedErrMsg := "PROJECT environment variable must be set for GCP managed prometheus client"
		if err.Error() != expectedErrMsg {
			t.Errorf("expected error message %q, got %q", expectedErrMsg, err.Error())
		}
	})
}

func TestNewGCPManagedPrometheusClient_EndpointResolution(t *testing.T) {
	origDefaultClientFunc := defaultClientFunc
	t.Cleanup(func() {
		defaultClientFunc = origDefaultClientFunc
	})
	defaultClientFunc = func(ctx context.Context, scopes ...string) (*http.Client, error) {
		return http.DefaultClient, nil
	}

	const testProject = "my-test-project"

	tests := []struct {
		name        string
		endpointEnv *string
		expectedURI string
	}{
		{
			name:        "Default endpoint: unset",
			endpointEnv: nil,
			expectedURI: "https://monitoring.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Default endpoint: empty string",
			endpointEnv: ptrString(""),
			expectedURI: "https://monitoring.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Custom endpoint with https scheme",
			endpointEnv: ptrString("https://staging-monitoring.sandbox.googleapis.com"),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Custom endpoint with http scheme",
			endpointEnv: ptrString("http://staging-monitoring.sandbox.googleapis.com"),
			expectedURI: "http://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Custom endpoint without scheme",
			endpointEnv: ptrString("staging-monitoring.sandbox.googleapis.com"),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Trailing slash handling with scheme",
			endpointEnv: ptrString("https://staging-monitoring.sandbox.googleapis.com/"),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Trailing slash handling without scheme",
			endpointEnv: ptrString("staging-monitoring.sandbox.googleapis.com/"),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Multiple trailing slashes",
			endpointEnv: ptrString("https://staging-monitoring.sandbox.googleapis.com///"),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Endpoint with leading and trailing whitespace",
			endpointEnv: ptrString("  https://staging-monitoring.sandbox.googleapis.com/  "),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
		{
			name:        "Endpoint without scheme with whitespace",
			endpointEnv: ptrString("  staging-monitoring.sandbox.googleapis.com/  "),
			expectedURI: "https://staging-monitoring.sandbox.googleapis.com/v1/projects/my-test-project/location/global/prometheus/api/v1/query",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(projectEnv, testProject)
			if tc.endpointEnv == nil {
				origVal, exists := os.LookupEnv(gcpMonitoringEndpointEnv)
				if exists {
					os.Unsetenv(gcpMonitoringEndpointEnv)
					t.Cleanup(func() {
						os.Setenv(gcpMonitoringEndpointEnv, origVal)
					})
				}
			} else {
				t.Setenv(gcpMonitoringEndpointEnv, *tc.endpointEnv)
			}

			client, err := NewGCPManagedPrometheusClient()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gcpClient, ok := client.(*gcpManagedPrometheusClient)
			if !ok {
				t.Fatalf("expected *gcpManagedPrometheusClient, got %T", client)
			}
			if gcpClient.uri != tc.expectedURI {
				t.Errorf("got URI %q, want %q", gcpClient.uri, tc.expectedURI)
			}
		})
	}
}

func TestNewGCPManagedPrometheusClient_ClientError(t *testing.T) {
	origDefaultClientFunc := defaultClientFunc
	t.Cleanup(func() {
		defaultClientFunc = origDefaultClientFunc
	})
	defaultClientFunc = func(ctx context.Context, scopes ...string) (*http.Client, error) {
		return nil, errors.New("auth failed")
	}

	t.Setenv(projectEnv, "my-project")
	client, err := NewGCPManagedPrometheusClient()
	if err == nil {
		t.Fatal("expected error from client initialization, got nil")
	}
	if client != nil {
		t.Fatalf("expected nil client on error, got %v", client)
	}
	if err.Error() != "auth failed" {
		t.Errorf("expected error %q, got %q", "auth failed", err.Error())
	}
}
