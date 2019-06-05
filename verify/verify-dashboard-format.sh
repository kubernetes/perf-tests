#!/bin/bash

# Copyright 2014 The Kubernetes Authors.
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

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
DASHBOARD_DIR="clusterloader2/pkg/prometheus/manifests/dashboards"

cd "${KUBE_ROOT}"

find_bad_files() {
  tmpfile=$(mktemp /tmp/formatted.XXXXXX)
  for file in $DASHBOARD_DIR/*.json ; do
    jq -M . "$file" > "$tmpfile"
    diff -q "$file" "$tmpfile" > /dev/null || echo "$file"
  done
  rm "$tmpfile"
}

fix_file() {
  tmpfile=$(mktemp /tmp/formatted.XXXXXX)
  jq -M . "$1" > "$tmpfile" && mv "$tmpfile" "$1"
  echo "Formatted: $1"
}

bad_files=$(find_bad_files)
if [[ "${1}" == "--fix" ]]; then
  for file in $bad_files; do
    fix_file "$file"
  done
elif [[ -n "${bad_files}" ]]; then
  echo "!!! ./verify-dashboard-format.sh --fix need to be run to format dashboards. "
  echo "Following dashboards are not formatted correctly: $bad_files"
fi
