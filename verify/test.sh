#!/bin/bash

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

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
KUBE_GO_PACKAGE="k8s.io/perf-tests"

find_test_dirs() {
  (
    cd ${KUBE_ROOT}
    find -L . -not \( \
      \( \
        -path './benchmark/vendor/*' \
        -o -path './clusterloader/e2e/*' \
        -o -path './clusterloader/vendor/*' \
        -o -path './compare/vendor/*' \
        -o -path './network/vendor/*' \
        -o -path './perfdash/vendor/*' \
        -o -path './slo-monitor/vendor/*' \
        -o -path './_logviewer/*' \
      \) -prune \
    \) -name '*_test.go' -print0 | xargs -0n1 dirname | sed "s|^\./|${KUBE_GO_PACKAGE}/|" | LC_ALL=C sort -u
  )
}

GO111MODULE=off go test $(find_test_dirs)
