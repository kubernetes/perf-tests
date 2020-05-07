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

# Find all directories with go.mod file,
# exluding go.mod from vendor/ and _logviewer
MODULE_BASED=$(find . -type d -name vendor -prune \
  -o -type f -name go.mod -printf "%h\n" \
  | grep -v "_logviewer" \
  | sort -u)

# Find all directrories with vendor/ directory
VENDOR_BASED=$(find . -type d -name vendor -printf "%h\n" | sort -u)

# There might be an overlap between $MODULE_BASED and $VENDOR_BASED.
# Find vendor only
VENDOR_ONLY=$(comm -13 \
  <(echo $MODULE_BASED | tr " " "\n") \
  <(echo $VENDOR_BASED | tr " " "\n"))

echo "Running tests with GO111MODULE=off..."
targets=$(echo $VENDOR_ONLY | tr " " "\n" \
  | sed -e "s|^\.|${KUBE_GO_PACKAGE}|" \
  | sed -e "s/$/.../")
set +x
status=0
GO111MODULE=off go test $targets || status=1

echo "Running tests with GO111MODULE=on..."
for mod in $MODULE_BASED; do
  (
    cd "${KUBE_ROOT}/${mod}"
    GO111MODULE=on go test ./...
  ) || status=1
done

exit $status
