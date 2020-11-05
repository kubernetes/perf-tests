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

set -o errexit
set -o nounset
set -o pipefail

function print_golang_commit {
  # Running "go version" on any of the extracted K8s binaries will give us
  # the version of Go that's been used to build the binaries.
  # In case of a non-standard, customly built Go, this will return the hash of
  # the commit that was used for that Go build (plus some unnecessary junk that
  # we remove below).
  local -r go_version=$(go version $GOPATH/src/k8s.io/perf-tests/kubernetes/client/bin/kubectl)
  local -r go_commit_hash=$(echo -n $go_version | awk '{ print $3 }' | cut -c2-)
  local -r dashline=$(printf %100s | tr " " "-")
  echo "${dashline}"
  echo "Output of 'go version' ran on one of the extracted K8s binaries: ${go_version}"
  echo "Golang commit used for building the K8s binaries: ${go_commit_hash}"
  echo "${dashline}"
}

print_golang_commit

$GOPATH/src/k8s.io/perf-tests/run-e2e.sh "$@"
