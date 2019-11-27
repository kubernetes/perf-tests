#!/bin/bash

# Copyright 2019 The Kubernetes Authors.
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

# Add firewall rule for Prometheus port (9090)
if [[ -n "${KUBE_GKE_NETWORK:-}" ]]; then
  if ! gcloud compute firewall-rules describe "${KUBE_GKE_NETWORK}-9090" > /dev/null 2>&1; then
    PROMETHEUS_RULE_NAME="${KUBE_GKE_NETWORK}-9090"
    echo "Prometheus firewall rule not found, creating..."
    echo COMMAND: gcloud compute firewall-rules create --network "${KUBE_GKE_NETWORK}" --source-ranges 0.0.0.0/0 --allow tcp:9090 "${PROMETHEUS_RULE_NAME}"
    gcloud compute firewall-rules create --network "${KUBE_GKE_NETWORK}" --source-ranges 0.0.0.0/0 --allow tcp:9090 "${PROMETHEUS_RULE_NAME}"
  fi
fi

# Try
set +o errexit
(
  set -o errexit
  SCRIPT_DIR=$(dirname "$0")
  echo COMMAND: $SCRIPT_DIR/run-e2e.sh ${@}
  $SCRIPT_DIR/run-e2e.sh ${@}
)

# Catch
ERR_CODE=$?
set -o errexit
echo Error code $ERR_CODE

if [[ -n "${PROMETHEUS_RULE_NAME:-}" ]]; then
  echo "Deleting Prometheus firewall rule..."
  echo COMMAND: gcloud compute firewall-rules delete "${PROMETHEUS_RULE_NAME}"
  gcloud compute firewall-rules delete "${PROMETHEUS_RULE_NAME}"
fi

exit $ERR_CODE
