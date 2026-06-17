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
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	// ErrorEmptyFile indicates that manifest file was empty.
	// Useful to distinguish where the manifast was empty or malformed.
	ErrorEmptyFile = errors.New("emptyfile")
)

// convertToObject converts array of bytes into unstructured object.
func convertToObject(raw []byte) (*unstructured.Unstructured, error) {
	if isEmpty(raw) {
		return nil, ErrorEmptyFile
	}
	obj := &unstructured.Unstructured{}
	_, _, err := scheme.Codecs.UniversalDeserializer().Decode(raw, nil, obj)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling error: %v", err)
	}
	return obj, nil
}

// convertToObjects converts array of bytes into a slice of unstructured objects.
func convertToObjects(raw []byte) ([]*unstructured.Unstructured, error) {
	if isEmpty(raw) {
		return nil, ErrorEmptyFile
	}
	var objs []*unstructured.Unstructured
	reader := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		var obj unstructured.Unstructured
		err := reader.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unmarshaling error: %v", err)
		}
		if len(obj.Object) == 0 {
			continue // Skip empty documents
		}
		objs = append(objs, &obj)
	}
	return objs, nil
}

func decodeInto(raw []byte, v interface{}) error {
	if err := yaml.NewYAMLOrJSONDecoder(bytes.NewBuffer(raw), 4096).Decode(v); err != nil {
		return fmt.Errorf("decoding failed: %v", err)
	}
	return nil
}

func isEmpty(raw []byte) bool {
	return strings.TrimSpace(string(raw[:])) == ""
}
