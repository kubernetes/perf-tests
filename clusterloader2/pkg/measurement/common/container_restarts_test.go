/*
Copyright 2021 The Kubernetes Authors.

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

package common

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/common/executors"
)

type rule struct {
	Expr   string `yaml:"expr"`
	Record string `yaml:"record"`
	Labels struct {
		Quantile string `yaml:"quantile"`
	} `yaml:"labels"`
}

type group struct {
	Name  string `yaml:"name"`
	Rules []rule `yaml:"rules"`
}

//prometheusRuleManifest mimics the structure of PrometheusRule object used by prometheus operator
//https://github.com/prometheus-operator/prometheus-operator/blob/main/pkg/apis/monitoring/v1/types.go#L1393
type prometheusRuleManifest struct {
	Spec struct {
		Groups []group `yaml:"groups"`
	} `yaml:"spec"`
}

func createRulesFile(rulesManifestFile string) (*os.File, error) {
	r, err := ioutil.ReadFile(rulesManifestFile)
	if err != nil {
		return nil, err
	}

	rulesManifest := new(prometheusRuleManifest)
	err = yaml.Unmarshal(r, rulesManifest)
	if err != nil {
		return nil, err
	}

	tempFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	b, err := yaml.Marshal(rulesManifest.Spec)
	if err != nil {
		return nil, err
	}

	_, err = tempFile.Write(b)
	if err != nil {
		return nil, err
	}
	return tempFile, nil
}

func TestContainerRestartsMeasurement(t *testing.T) {
	splitter := func(yamlLines []string) string {
		return strings.Join(yamlLines, "\n")
	}

	cases := []struct {
		name               string
		config             *measurement.Config
		hasError           bool
		testSeriesFile     string
		testSeriesDuration time.Duration
	}{
		{
			name:               "no_restarts",
			hasError:           false,
			testSeriesFile:     "no_restarts.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
		},
		{
			name:               "double_restart_of_apiserver/violation",
			hasError:           true,
			testSeriesFile:     "double_restart_of_apiserver.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
		},
		{
			name:               "double_restart_of_apiserver/default_allowed_restarts",
			hasError:           false,
			testSeriesFile:     "double_restart_of_apiserver.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{
					"defaultAllowedRestarts": 2,
				},
			},
		},
		{
			name:               "double_restart_of_apiserver/custom_allowed_restarts",
			hasError:           false,
			testSeriesFile:     "double_restart_of_apiserver.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customAllowedRestarts": splitter([]string{
						"- container: kube-apiserver",
						"  pod: kube-apiserver",
						"  namespace: kube-system",
						"  allowedRestarts: 2",
					}),
				},
			},
		},
		{
			name:               "two_apiserver_replicas_restarts/violation",
			hasError:           true,
			testSeriesFile:     "two_apiserver_replicas_restarts.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{},
			},
		},
		{
			name:               "two_apiserver_replicas_restarts/custom_allowed_restarts_with_regex",
			hasError:           false,
			testSeriesFile:     "two_apiserver_replicas_restarts.yaml",
			testSeriesDuration: 10 * time.Minute,
			config: &measurement.Config{
				Params: map[string]interface{}{
					"customAllowedRestarts": splitter([]string{
						"- container: kube-apiserver",
						"  pod: kube-apiserver-*",
						"  namespace: kube-system",
						"  allowedRestarts: 2",
					}),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := createRulesFile("../../prometheus/manifests/prometheus-rules.yaml")
			if err != nil {
				t.Fatalf("failed to create rules file: %v", err)
			}
			defer os.Remove(f.Name())
			executor, err := executors.NewPromqlExecutor(fmt.Sprintf("slos/testdata/container_restarts/%s", tc.testSeriesFile), f.Name())
			if err != nil {
				t.Fatalf("failed to create rules file: %v", err)
			}
			defer executor.Close()
			gatherer := &containerRestartsGatherer{}
			start := time.Unix(0, 0).UTC()
			end := start.Add(tc.testSeriesDuration)
			_, err = gatherer.Gather(executor, start, end, tc.config)
			if tc.hasError {
				assert.NotNil(t, err, "wanted error, but got none")
			} else {
				assert.Nil(t, err, "wanted no error, but got %v", err)
			}
		})
	}
}
