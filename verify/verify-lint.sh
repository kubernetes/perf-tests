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

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
cd "$KUBE_ROOT"

# WARNING: we exclude the following paths from linting
# because the CI lacks the required golang version (1.20)
EXCLUDE_PATHS=("util-images/watch-list")

if [[ ${#EXCLUDE_PATHS[@]} -ne 0 ]]; then
  echo "WARNING: The following paths will be excluded from linting: ${EXCLUDE_PATHS[@]}"
fi

# Find all directories with go.mod file,
# exluding go.mod from vendor/ and _logviewer
MODULE_BASED=$(find . -type d -name vendor -prune \
  -o -type f -name go.mod -printf "%h\n" \
  | sort -u \
  | grep -v -E $(printf -- "-e %s\n" "${EXCLUDE_PATHS[@]}"))

set +e
status=0

for mod in $MODULE_BASED; do
  (
    cd "${mod}"
    GO111MODULE=on golangci-lint run ./...
  ) || status=1
done

exit $status
