#!/usr/bin/env bash

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

print-usage-and-exit() {
  >&2 echo "Usage:"
  >&2 echo "    $0 delete|list <comma-sep-projects> <comma-sep-prefixes> <days-old>"
  >&2 echo "Example:"
  >&2 echo "    $0 list k8s-e2e-gce-1-1,k8s-e2e-gce-1-2  ci-e2e-scalability,ci-e2e-kubemark 30"
  exit "$1"
}

list-old-snapshots() {
  project=$1
  regexp=$2
  days_old=$3
  (
    set -o xtrace
    gcloud compute snapshots list \
      --project "$project" \
      --filter="creationTimestamp < -P${days_old}D AND name~\"${regexp}\"" \
      --format="value(name)"
  )
}

process-old-snapshots() {
  command=$1
  project=$2
  regexp=$3
  days_old=$4
  for snapshot in $(list-old-snapshots "$project" "$regexp" "$days_old"); do
    if [[ "$command" == "delete" ]]; then
      echo "Removing $snapshot ..."
      gcloud compute snapshots delete -q --project "$project" "$snapshot"
    else
      echo "Found: $snapshot"
    fi
  done
}

main() {
  if [ "$#" -ne 4 ]; then
    >&2 echo "Wrong number of parameters, expected 4, got $#"
    print-usage-and-exit 1
  fi

  command=$1
  projects=$2
  prefixes=$3
  days_old=$4

  if ! [[ "$command" =~ ^(list|delete)$ ]]; then
    >&2 echo "Invalid command: $command"
    print-usage-and-exit 2
  fi
  if ! [[ "$projects" =~ ^[a-z0-9,-]+$ ]]; then
    >&2 echo "Illegal characters in projects parameter: $projects"
    print-usage-and-exit 3
  fi
  if ! [[ "$prefixes" =~ ^[a-z0-9,.-]+$ ]]; then
    >&2 echo "Illegal characters in prefixes parameter: $prefixes"
    print-usage-and-exit 4
  fi
  if ! [[ "$days_old" =~ ^[0-9]+$ ]]; then
    >&2 echo "Days-old parameter must be an integer: $days_old"
    print-usage-and-exit 5
  fi

  prefixes_alt="${prefixes//,/|}"
  regexp="^(${prefixes_alt}).*-[0-9]{19}$"
  echo "Looking for snapshots matched by $regexp"

  IFS=',' read -ra projects_arr <<< "$projects"

  for project in "${projects_arr[@]}"; do
    echo "Processing project $project"
    process-old-snapshots "$command" "$project" "$regexp" "$days_old"
  done
}

main "$@"
