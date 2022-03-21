#!/usr/bin/env bash

# Copyright 2022 The Kubernetes Authors.
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

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)

print-usage-and-exit() {
  >&2 echo "Usage:"
  >&2 echo "    $0 delete|list <comma-sep-prefixes> <days-old> <comma-sep-boskos-pools> <boskos-resources-file>"
  >&2 echo "Example:"
  >&2 echo "    $0 list ci-e2e-scalability,ci-e2e-kubemark 30 scalability-project ./boskos-resources.yaml"
  exit "$1"
}

main() {
  if [ "$#" -ne 5 ]; then
    >&2 echo "Wrong number of parameters, expected 5, got $#"
    print-usage-and-exit 1
  fi

  command=$1
  prefixes=$2
  days_old=$3
  boskos_pools=$4
  boskos_file=$5

  if ! [[ "$boskos_pools" =~ ^[a-z0-9,-]+$ ]]; then
    >&2 echo "Illegal characters in projects parameter: $boskos_pools"
    print-usage-and-exit 2
  fi

  if ! [[ -f "$boskos_file" ]]; then
    >&2 echo "$boskos_file file does not exist"
    print-usage-and-exit 3
  fi

  projects=()

  IFS=',' read -ra boskos_pools_arr <<< "$boskos_pools"
  for boskos_pool in "${boskos_pools_arr[@]}"; do
    readarray boskos_projects < \
      <(yq ".resources.[] | select (.type == \"$boskos_pool\") | .names.[]" "$boskos_file")
    if [ ${#boskos_projects[@]} -eq 0 ]; then
      >&2 echo "Could not find any project under boskos pool $boskos_pool"
      exit 4
    fi

    for project in "${boskos_projects[@]}"; do
      projects+=("$(echo -e "$project" | tr -d '[:space:]')")
    done
  done

  projects_str=$(IFS=','; echo "${projects[*]}")

  (
    set -o xtrace
    "$SCRIPT_DIR/clean-up-old-snapshots.sh" \
      "$command" "$projects_str" "$prefixes" "$days_old"
  )
}

main "$@"
