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
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

// ErrKeyNotFound is returned when key doesn't exists in a map.
type ErrKeyNotFound struct {
	key string
}

// Erros is an error interface implementation.
func (e *ErrKeyNotFound) Error() string {
	return fmt.Sprintf("key %s not found", e.key)
}

// IsErrKeyNotFound returns true only if error type is ErrKeyNotFound.
func IsErrKeyNotFound(err error) bool {
	_, isErrKeyNotFound := err.(*ErrKeyNotFound)
	return isErrKeyNotFound
}

// GetString tries to return value from map cast to string type. If value doesn't exist, error is returned.
func GetString(dict map[string]interface{}, key string) (string, error) {
	return getString(dict, key)
}

// GetInt tries to return value from map cast to int type. If value doesn't exist, error is returned.
func GetInt(dict map[string]interface{}, key string) (int, error) {
	return getInt(dict, key)
}

// GetFloat64 tries to return value from map cast to float64 type. If value doesn't exist, error is returned.
func GetFloat64(dict map[string]interface{}, key string) (float64, error) {
	return getFloat64(dict, key)
}

// GetDuration tries to return value from map cast to duration type. If value doesn't exist, error is returned.
func GetDuration(dict map[string]interface{}, key string) (time.Duration, error) {
	return getDuration(dict, key)
}

// GetBool tries to return value from map cast to bool type. If value doesn't exist, error is returned.
func GetBool(dict map[string]interface{}, key string) (bool, error) {
	return getBool(dict, key)
}

// GetMap tries to return value from map of type map. If value doesn't exist, error is returned.
func GetMap(dict map[string]interface{}, key string) (map[string]interface{}, error) {
	return getMap(dict, key)
}

// GetStringOrDefault tries to return value from map cast to string type. If value doesn't exist default value is used.
func GetStringOrDefault(dict map[string]interface{}, key string, defaultValue string) (string, error) {
	value, err := getString(dict, key)
	if IsErrKeyNotFound(err) {
		return defaultValue, nil
	}
	return value, err
}

// GetIntOrDefault tries to return value from map cast to int type. If value doesn't exist default value is used.
func GetIntOrDefault(dict map[string]interface{}, key string, defaultValue int) (int, error) {
	value, err := getInt(dict, key)
	if IsErrKeyNotFound(err) {
		return defaultValue, nil
	}
	return value, err
}

// GetFloat64OrDefault tries to return value from map cast to float64 type. If value doesn't exist default value is used.
func GetFloat64OrDefault(dict map[string]interface{}, key string, defaultValue float64) (float64, error) {
	value, err := getFloat64(dict, key)
	if IsErrKeyNotFound(err) {
		return defaultValue, nil
	}
	return value, err
}

// GetDurationOrDefault tries to return value from map cast to duration type. If value doesn't exist default value is used.
func GetDurationOrDefault(dict map[string]interface{}, key string, defaultValue time.Duration) (time.Duration, error) {
	value, err := getDuration(dict, key)
	if IsErrKeyNotFound(err) {
		return defaultValue, nil
	}
	return value, err
}

// GetBoolOrDefault tries to return value from map cast to bool type. If value doesn't exist default value is used.
func GetBoolOrDefault(dict map[string]interface{}, key string, defaultValue bool) (bool, error) {
	value, err := getBool(dict, key)
	if IsErrKeyNotFound(err) {
		return defaultValue, nil
	}
	return value, err
}

func getMap(dict map[string]interface{}, key string) (map[string]interface{}, error) {
	value, exists := dict[key]
	if !exists || value == nil {
		return nil, &ErrKeyNotFound{key}
	}

	mapValue, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("type assertion error: %v is not a string", value)
	}
	return mapValue, nil
}

func getString(dict map[string]interface{}, key string) (string, error) {
	value, exists := dict[key]
	if !exists || value == nil {
		return "", &ErrKeyNotFound{key}
	}

	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("type assertion error: %v is not a string", value)
	}
	return stringValue, nil
}

func getInt(dict map[string]interface{}, key string) (int, error) {
	value, exists := dict[key]
	if !exists {
		return 0, &ErrKeyNotFound{key}
	}

	intValue, ok := value.(int)
	if ok {
		return intValue, nil
	}
	// Types from interface{} create from json cannot be cast directly to int.
	floatValue, ok := value.(float64)
	if ok {
		return int(floatValue), nil
	}
	stringValue, ok := value.(string)
	if ok {
		if i, err := strconv.Atoi(stringValue); err != nil {
			return i, nil
		}
	}
	return 0, fmt.Errorf("type assertion error: %v is not an int", value)
}

func getFloat64(dict map[string]interface{}, key string) (float64, error) {
	value, exists := dict[key]
	if !exists {
		return 0, &ErrKeyNotFound{key}
	}

	floatValue, ok := value.(float64)
	if ok {
		return floatValue, nil
	}
	stringValue, ok := value.(string)
	if ok {
		if f, err := strconv.ParseFloat(stringValue, 64); err != nil {
			return f, nil
		}
	}
	return 0, fmt.Errorf("type assertion error: %v is not a float", value)
}

func getDuration(dict map[string]interface{}, key string) (time.Duration, error) {
	durationString, err := getString(dict, key)
	if err != nil {
		return 0, err
	}

	duration, err := time.ParseDuration(durationString)
	if err != nil {
		return 0, fmt.Errorf("parsing duration error: %v", err)
	}
	return duration, nil
}

func getBool(dict map[string]interface{}, key string) (bool, error) {
	value, exists := dict[key]
	if !exists {
		return false, &ErrKeyNotFound{key}
	}

	boolValue, ok := value.(bool)
	if ok {
		return boolValue, nil
	}
	stringValue, ok := value.(string)
	if ok {
		if b, err := strconv.ParseBool(stringValue); err != nil {
			return b, nil
		}
	}
	return false, fmt.Errorf("type assertion error: %v is not a bool", value)
}

// PrettyPrintJSON converts given data into formatted json.
func PrettyPrintJSON(data interface{}) (string, error) {
	output := &bytes.Buffer{}
	if err := json.NewEncoder(output).Encode(data); err != nil {
		return "", fmt.Errorf("building encoder error: %v", err)
	}
	formatted := &bytes.Buffer{}
	if err := json.Indent(formatted, output.Bytes(), "", "  "); err != nil {
		return "", fmt.Errorf("indenting error: %v", err)
	}
	return formatted.String(), nil
}

// CopyMap copies values from one map to the other.
func CopyMap(src, dest map[string]interface{}) {
	for k, v := range src {
		dest[k] = v
	}
}

// CloneMap returns clone of the provided map.
func CloneMap(src map[string]interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	CopyMap(src, m)
	return m
}

// RandomDNS1123String generates random string of a given length.
func RandomDNS1123String(length int) string {
	characters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, length)
	for i := range s {
		s[i] = characters[rand.Intn(len(characters))]
	}
	return string(s)
}
