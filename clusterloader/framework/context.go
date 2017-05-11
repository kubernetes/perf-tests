/*
Copyright 2017 The Kubernetes Authors.

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

package framework

import (
	"os"

	"github.com/spf13/viper"
)

// Context is the root config struct
type Context struct {
	ClusterLoader struct {
		Projects   []ClusterLoader
		TuningSets []TuningSet
	}
}

// TuningSets is custom slice type so we can define methods on it
type TuningSets []TuningSet

// ClusterLoader struct only used for Cluster Loader test config
type ClusterLoader struct {
	Number    int `mapstructure:"num"`
	Basename  string
	Tuning    string
	Pods      []ClusterLoaderObject
	RCs       []ClusterLoaderObject
	Templates []ClusterLoaderObject
}

// ClusterLoaderObject is nested object type for cluster loader struct
type ClusterLoaderObject struct {
	Total    int
	Number   int `mapstructure:"num"`
	Image    string
	Basename string
	File     string
	Label    string
}

// TuningSet is nested type for controlling Cluster Loader deployment pattern
type TuningSet struct {
	Name      string
	Project   TuningSetObject
	Pods      TuningSetObject
	Templates TuningSetObject
}

// TuningSetObject is shared struct for Pods & Templates
type TuningSetObject struct {
	Stepping struct {
		StepSize int
		Pause    string
		Timeout  string
	}
	RateLimit struct {
		Delay string
	}
}

// ConfigContext variable of type Context
var ConfigContext Context

// ParseConfig will complete flag parsing as well as viper tasks
func ParseConfig(config string) {
	// This must be done after common flags are registered, since Viper is a flag option.
	viper.SetConfigName(config)
	viper.AddConfigPath(os.Getenv("GOPATH") + "/src/k8s.io/perf-tests/clusterloader")
	viper.ReadInConfig()
	viper.Unmarshal(&ConfigContext)
}
