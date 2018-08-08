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

package util

import (
	"fmt"
	"time"
)

// GetString tries to return value from map cast to string type. If value doesn't exist, error is returned.
func GetString(dict map[string]interface{}, key string) (string, error) {
	return getString(dict, key, "", fmt.Errorf("key %s not found", key))
}

// GetInt tries to return value from map cast to int type. If value doesn't exist, error is returned.
func GetInt(dict map[string]interface{}, key string) (int, error) {
	return getInt(dict, key, 0, fmt.Errorf("key %s not found", key))
}

// GetFloat64 tries to return value from map cast to float64 type. If value doesn't exist, error is returned.
func GetFloat64(dict map[string]interface{}, key string) (float64, error) {
	return getFloat64(dict, key, 0, fmt.Errorf("key %s not found", key))
}

// GetDuration tries to return value from map cast to duration type. If value doesn't exist, error is returned.
func GetDuration(dict map[string]interface{}, key string) (time.Duration, error) {
	return getDuration(dict, key, 0, fmt.Errorf("key %s not found", key))
}

// GetStringOrDefault tries to return value from map cast to string type. If value doesn't exist default value is used.
func GetStringOrDefault(dict map[string]interface{}, key string, defaultValue string) (string, error) {
	return getString(dict, key, defaultValue, nil)
}

// GetIntOrDefault tries to return value from map cast to int type. If value doesn't exist default value is used.
func GetIntOrDefault(dict map[string]interface{}, key string, defaultValue int) (int, error) {
	return getInt(dict, key, defaultValue, nil)
}

// GetFloat64OrDefault tries to return value from map cast to float64 type. If value doesn't exist default value is used.
func GetFloat64OrDefault(dict map[string]interface{}, key string, defaultValue float64) (float64, error) {
	return getFloat64(dict, key, defaultValue, nil)
}

// GetDurationOrDefault tries to return value from map cast to duration type. If value doesn't exist default value is used.
func GetDurationOrDefault(dict map[string]interface{}, key string, defaultValue time.Duration) (time.Duration, error) {
	return getDuration(dict, key, defaultValue, nil)
}

func getString(dict map[string]interface{}, key string, defaultValue string, defaultError error) (string, error) {
	value, exists := dict[key]
	if !exists {
		return defaultValue, defaultError
	}

	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("type assertion error: %v is not a string", value)
	}
	return stringValue, nil
}

func getInt(dict map[string]interface{}, key string, defaultValue int, defaultError error) (int, error) {
	value, exists := dict[key]
	if !exists {
		return defaultValue, defaultError
	}

	// Types from interface{} create from json cannot be cast directly to int.
	floatValue, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("type assertion error: %v of is not an int", value)
	}
	return int(floatValue), nil
}

func getFloat64(dict map[string]interface{}, key string, defaultValue float64, defaultError error) (float64, error) {
	value, exists := dict[key]
	if !exists {
		return defaultValue, defaultError
	}

	floatValue, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("type assertion error: %v of is not an int", value)
	}
	return floatValue, nil
}

func getDuration(dict map[string]interface{}, key string, defaultValue time.Duration, defaultError error) (time.Duration, error) {
	defaultErr := fmt.Errorf("key %s not found", key)
	durationString, err := getString(dict, key, "", defaultErr)
	if err != nil {
		if err == defaultErr {
			return defaultValue, defaultError
		}
		return 0, err
	}

	duration, err := time.ParseDuration(durationString)
	if err != nil {
		return 0, fmt.Errorf("parsing duration error: %v", err)
	}
	return duration, nil
}
