# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

BINARY := build/benchmark
SOURCES := $(shell find pkg -name '*.go' | grep -v "_test.go")
TESTS := $(shell find pkg -name '*_test.go')

all: benchmark

godep:
	go get -u github.com/tools/godep

benchmark: godep cmd/benchmark.go $(SOURCES)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 godep go build -a -o $(BINARY) cmd/benchmark.go

test: godep $(TESTS)
	godep go test k8s.io/perf-tests/benchmark/pkg/... -v

clean:
	rm -f $(BINARY)

.PHONY: all godep benchmark test clean
