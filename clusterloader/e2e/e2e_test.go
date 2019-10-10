/*
Copyright 2016 The Kubernetes Authors.

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

package clusterloader

import (
	"testing"

	"github.com/spf13/viper"
	"k8s.io/kubernetes/test/e2e/framework"
	_ "k8s.io/perf-tests/clusterloader"
	clframe "k8s.io/perf-tests/clusterloader/framework"
)

func init() {
	framework.ViperizeFlags()
	clframe.ParseConfig(framework.TestContext.Viper)
}

func TestE2E(t *testing.T) {
	viper.Debug()
	RunE2ETests(t)
}
