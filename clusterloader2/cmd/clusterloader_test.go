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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/perf-tests/clusterloader2/api"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/test"
)

type dumpTestContext struct {
	test.Context
	clusterLoaderConfig *config.ClusterLoaderConfig
	testScenario        *api.TestScenario
}

func (c *dumpTestContext) GetClusterLoaderConfig() *config.ClusterLoaderConfig {
	return c.clusterLoaderConfig
}

func (c *dumpTestContext) GetTestScenario() *api.TestScenario {
	return c.testScenario
}

func TestDumpTestConfigUsesScenarioIdentifier(t *testing.T) {
	reportDir := t.TempDir()
	clusterLoaderConfig := &config.ClusterLoaderConfig{ReportDir: reportDir}
	testConfig := &api.Config{Name: "density"}

	for _, identifier := range []string{"vanilla", "pod-affinity"} {
		ctx := &dumpTestContext{
			clusterLoaderConfig: clusterLoaderConfig,
			testScenario:        &api.TestScenario{Identifier: identifier},
		}
		if err := dumpTestConfig(ctx, testConfig); err != nil {
			t.Fatalf("dumpTestConfig() error = %v", err)
		}
	}

	for _, identifier := range []string{"vanilla", "pod-affinity"} {
		filePath := filepath.Join(reportDir, "generatedConfig_density_"+identifier+".yaml")
		if _, err := os.Stat(filePath); err != nil {
			t.Errorf("expected config artifact %q: %v", filePath, err)
		}
	}
}

func TestRedactedKubeConfig(t *testing.T) {
	const kubeConfigTemplate = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
%s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
%s
`
	const defaultCluster = "    server: https://test.example.com:6443"
	const defaultUser = "    username: admin"

	tests := []struct {
		name        string
		cluster     string
		user        string
		wantMissing []string
		wantPresent []string
	}{
		{
			name:        "no credentials",
			wantPresent: []string{"server: https://test.example.com:6443", "current-context: test", "username: admin"},
		},
		{
			name:        "bearer token",
			user:        "    token: secret-token",
			wantMissing: []string{"secret-token"},
			wantPresent: []string{"token: REDACTED"},
		},
		{
			name:        "basic auth password",
			user:        "    username: admin\n    password: secret-password",
			wantMissing: []string{"secret-password"},
			wantPresent: []string{"username: admin"},
		},
		{
			name:        "client certificate and key data",
			user:        "    client-certificate-data: c2VjcmV0LWNlcnQ=\n    client-key-data: c2VjcmV0LWtleQ==",
			wantMissing: []string{"c2VjcmV0LWNlcnQ=", "c2VjcmV0LWtleQ=="},
		},
		{
			name:        "certificate authority data",
			cluster:     defaultCluster + "\n    certificate-authority-data: c2VjcmV0LWNh",
			wantMissing: []string{"c2VjcmV0LWNh"},
		},
		{
			name:        "auth provider config",
			user:        "    auth-provider:\n      name: oidc\n      config:\n        id-token: secret-id-token\n        refresh-token: secret-refresh-token",
			wantMissing: []string{"secret-id-token", "secret-refresh-token"},
			wantPresent: []string{"name: oidc"},
		},
		{
			name:        "exec plugin args and env",
			user:        "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: get-token\n      args:\n      - --client-secret=secret-client-secret\n      env:\n      - name: API_KEY\n        value: secret-api-key",
			wantMissing: []string{"secret-client-secret", "secret-api-key"},
			wantPresent: []string{"command: get-token", "name: API_KEY"},
		},
		{
			name:        "proxy url userinfo",
			cluster:     defaultCluster + "\n    proxy-url: http://proxyuser:secret-proxy-password@proxy.example.com:3128",
			wantMissing: []string{"proxyuser", "secret-proxy-password"},
			wantPresent: []string{"proxy-url: http://proxy.example.com:3128"},
		},
		{
			name:        "unparsable proxy url",
			cluster:     defaultCluster + "\n    proxy-url: http://proxyuser:secret-proxy-password@proxy.example.com:bad-port",
			wantMissing: []string{"proxyuser", "secret-proxy-password"},
			wantPresent: []string{"proxy-url: REDACTED"},
		},
		{
			name:        "server url userinfo",
			cluster:     "    server: https://serveruser:secret-server-password@test.example.com:6443",
			wantMissing: []string{"serveruser", "secret-server-password"},
			wantPresent: []string{"server: https://test.example.com:6443"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, user := tt.cluster, tt.user
			if cluster == "" {
				cluster = defaultCluster
			}
			if user == "" {
				user = defaultUser
			}
			path := filepath.Join(t.TempDir(), "kubeconfig")
			if err := os.WriteFile(path, []byte(fmt.Sprintf(kubeConfigTemplate, cluster, user)), 0600); err != nil {
				t.Fatalf("writing kubeconfig: %v", err)
			}

			got, err := redactedKubeConfig(path)
			if err != nil {
				t.Fatalf("redactedKubeConfig() error = %v", err)
			}
			for _, s := range tt.wantMissing {
				if strings.Contains(got, s) {
					t.Errorf("redactedKubeConfig() contains %q, want it redacted:\n%s", s, got)
				}
			}
			for _, s := range tt.wantPresent {
				if !strings.Contains(got, s) {
					t.Errorf("redactedKubeConfig() is missing %q:\n%s", s, got)
				}
			}
		})
	}
}
