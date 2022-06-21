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

package executors

import (
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v2"
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
