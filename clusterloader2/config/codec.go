/*
Copyright 2018 The Kubernetes Authors.

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

package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"k8s.io/perf-tests/clusterloader2/api"
)

// ReadConfig creates config from file specified by the given path.
func ReadConfig(path string) (*api.Config, error) {
	// This must be done after common flags are registered, since Viper is a flag option.
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	viper.SetConfigName(strings.TrimSuffix(base, ext))
	viper.AddConfigPath(filepath.Dir(path))
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config failed: %v", err)
	}
	var config api.Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("unmarshaling failed: %v", err)
	}
	return &config, nil
}
