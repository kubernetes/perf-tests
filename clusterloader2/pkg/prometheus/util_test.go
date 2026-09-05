/*
Copyright 2019 The Kubernetes Authors.

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

package prometheus

import (
	"testing"

	"k8s.io/perf-tests/clusterloader2/pkg/config"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

func TestVerifySnapshotName(t *testing.T) {
	tests := []struct {
		name    string
		isValid bool
	}{
		{"disk-name", true},
		{"disk_name", false},
		{"disk12345", true},
		{"123242345", false},
	}

	for _, test := range tests {
		err := VerifySnapshotName(test.name)
		if test.isValid != (err == nil) {
			t.Errorf("Incorrect validation result of %s, got: %v, want: %v",
				test.name, (err == nil), test.isValid)
		}
	}
}

func TestValidatePrometheusFlagsUseExisting(t *testing.T) {
	t.Cleanup(func() { prom.ConfigureInClusterProxy(prom.DefaultProxyScheme, prom.DefaultServiceName) })

	tests := []struct {
		name    string
		cfg     config.PrometheusConfig
		wantErr bool
	}{
		{
			name: "defaults",
			cfg:  config.PrometheusConfig{ProxyScheme: "http", ServiceName: "prometheus-k8s"},
		},
		{
			name: "use existing https",
			cfg: config.PrometheusConfig{
				UseExistingServer: true,
				ProxyScheme:       "https",
				ServiceName:       "prometheus-k8s-shard-0",
			},
		},
		{
			name: "both enable and use existing",
			cfg: config.PrometheusConfig{
				EnableServer:      true,
				UseExistingServer: true,
				ProxyScheme:       "http",
			},
			wantErr: true,
		},
		{
			name:    "bad scheme",
			cfg:     config.PrometheusConfig{ProxyScheme: "ftp"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errList := ValidatePrometheusFlags(&tc.cfg)
			gotErr := !errList.IsEmpty()
			if gotErr != tc.wantErr {
				t.Fatalf("ValidatePrometheusFlags() error=%v (%s), wantErr=%v", gotErr, errList.String(), tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got := prom.InClusterProxyScheme(); got != tc.cfg.ProxyScheme {
				t.Errorf("scheme = %q, want %q", got, tc.cfg.ProxyScheme)
			}
			if tc.cfg.ServiceName != "" && prom.InClusterServiceName() != tc.cfg.ServiceName {
				t.Errorf("service = %q, want %q", prom.InClusterServiceName(), tc.cfg.ServiceName)
			}
		})
	}
}
