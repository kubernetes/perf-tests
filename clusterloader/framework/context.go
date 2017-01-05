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

package framework

import (
	"time"

	"github.com/spf13/viper"
)

// ContextType is the root config struct
type ContextType struct {
	ClusterLoader struct {
		Projects   []ClusterLoaderType
		TuningSets []TuningSetType
	}
}

// ClusterLoaderType struct only used for Cluster Loader test config
type ClusterLoaderType struct {
	Number    int `mapstructure:"num"`
	Basename  string
	Tuning    string
	Pods      []ClusterLoaderObjectType
	Templates []ClusterLoaderObjectType
}

// ClusterLoaderObjectType is nested object type for cluster loader struct
type ClusterLoaderObjectType struct {
	Total    int
	Number   int `mapstructure:"num"`
	Image    string
	Basename string
	File     string
}

// TuningSetType is nested type for controlling Cluster Loader deployment pattern
type TuningSetType struct {
	Name      string
	Pods      TuningSetObjectType
	Templates TuningSetObjectType
}

// TuningSetObjectType is shared struct for Pods & Templates
type TuningSetObjectType struct {
	Stepping struct {
		StepSize int
		Pause    time.Duration
		Timeout  time.Duration
	}
	RateLimit struct {
		Delay time.Duration
	}
}

// ConfigContext variable of type ContextType
var ConfigContext ContextType

// ParseConfig will complete flag parsing as well as viper tasks
func ParseConfig(config string) {
	// This must be done after common flags are registered, since Viper is a flag option.
	viper.SetConfigName(config)
	viper.AddConfigPath(".")
	viper.ReadInConfig()
	viper.Unmarshal(&ConfigContext)
}
