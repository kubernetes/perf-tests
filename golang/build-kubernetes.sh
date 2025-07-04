#!/bin/bash

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

set -euxo pipefail

# Initialize necessary environment variables.
function init {
  export GOROOT=$ROOT_DIR/golang
}

function build_kubernetes {
  cd $ROOT_DIR/k8s.io/kubernetes

  # Ensure we build against latest golang version.
  echo -n "devel" > .go-version

  # Create a temp commit
  git add .
  git config user.email "test@test-email.com"
  git config user.name "Test Name"
  git commit -m "Switch .go-version to 'devel'"

  # Build Kubernetes using our kube-cross image.
  # Also pass GOTOOLCHAIN=local to make sure kube-build
  # uses the golang version built locally in build-go.sh.
  make quick-release
}

init
build_kubernetes
