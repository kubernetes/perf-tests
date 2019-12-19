#!/bin/bash

# Copyright 2018 The Kubernetes Authors.
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

CLUSTERLOADER_ROOT=$(dirname "${BASH_SOURCE}")
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"
export KUBEMARK_ROOT_KUBECONFIG="${KUBEMARK_ROOT_KUBECONFIG:-${HOME}/.kube/config}"

# Deploy the GCP PD CSI Driver if required
if [[ "${DEPLOY_GCI_DRIVER:-false}" == "true" ]]; then
   if [[ -z "${E2E_GOOGLE_APPLICATION_CREDENTIALS:-}" ]]; then
      echo "Env var E2E_GOOGLE_APPLICATION_CREDENTIALS must be set to deploy driver"
      exit 1
   fi
   kubectl create secret generic cloud-sa --from-file="${E2E_GOOGLE_APPLICATION_CREDENTIALS:-}"
   kubectl apply -f ${CLUSTERLOADER_ROOT}/drivers/gcp-csi-driver-stable.yaml
   kubectl wait pods -l app=gcp-compute-persistent-disk-csi-driver --for condition=Ready --timeout=300s
fi

cd ${CLUSTERLOADER_ROOT}/ && go build -o clusterloader './cmd/'
./clusterloader --alsologtostderr "$@"
