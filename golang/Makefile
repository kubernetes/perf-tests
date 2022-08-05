# Copyright 2020 The Kubernetes Authors.
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

.PHONY: build-go build push

export GCS_BUCKET?=k8s-infra-scale-golang-builds
export GO_COMPILER_PKG?=go1.18.5.linux-amd64.tar.gz
export GO_COMPILER_URL?=https://dl.google.com/go/$(GO_COMPILER_PKG)
export ROOT_DIR?=/home/prow/go/src

all: push

build-go:
	./build-go.sh

build: build-go
	./build-kubernetes.sh

push: build
	./push.sh
