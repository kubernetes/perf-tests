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

package main

import (
	"fmt"

	"github.com/golang/protobuf/ptypes"
	// Imports in repo are absolute.
	"k8s.io/perf-tests/logviewer/example/protos"
	"k8s.io/perf-tests/logviewer/example/protos2"
)

func main() {
	x := &protos.TestMessage{
		Nested: &protos2.TestMessage2{},
		Test:   ptypes.TimestampNow(),
	}
	fmt.Printf("%+v\n", x)
}
