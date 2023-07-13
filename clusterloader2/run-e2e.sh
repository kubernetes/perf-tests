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

CLUSTERLOADER_ROOT=$(dirname "${BASH_SOURCE[0]}")
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"
# "${HOME}/.kube/config" always (both in kubemark and non-kubemark) points to "real"/root cluster.
# TODO: eventually we need to move to use cloud-provider-gcp to bring up cluster which have pdcsi by default
export CSI_DRIVER_KUBECONFIG="${HOME}/.kube/config"
export KUBEMARK_ROOT_KUBECONFIG="${KUBEMARK_ROOT_KUBECONFIG:-${HOME}/.kube/config}"

export AZUREDISK_CSI_DRIVER_VERSION="${AZUREDISK_CSI_DRIVER_VERSION:-master}"
export AZUREDISK_CSI_DRIVER_INSTALL_URL="${AZUREDISK_CSI_DRIVER_INSTALL_URL:-https://raw.githubusercontent.com/kubernetes-sigs/azuredisk-csi-driver/${AZUREDISK_CSI_DRIVER_VERSION}/deploy/install-driver.sh}"
export WINDOWS_USE_HOST_PROCESS_CONTAINERS=true

# Deploy the GCE PD CSI Driver if required
if [[ "${DEPLOY_GCI_DRIVER:-false}" == "true" ]]; then
   if [[ -n "${E2E_GOOGLE_APPLICATION_CREDENTIALS:-}" ]]; then
      kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" apply -f "${CLUSTERLOADER_ROOT}"/drivers/gcp-csi-driver-stable.yaml
      kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" create secret generic cloud-sa --from-file=cloud-sa.json="${E2E_GOOGLE_APPLICATION_CREDENTIALS:-}" -n gce-pd-csi-driver
   else
      echo "Env var E2E_GOOGLE_APPLICATION_CREDENTIALS is unset."
      echo "Falling back to using Application Default Credentials for GCE PD CSI driver deployment."
      if [[ ! -x "$(command -v yq)" ]]; then
         echo "yq must be installed to set up GCE PD CSI Driver with Application Default Credentials."
         echo "Please install this tool from https://github.com/mikefarah/yq."
         exit 1
      fi
      # Running yq to patch GCE PD CSI Driver manifests so that it runs using
      # Application Default Credentials instead of creating service account
      # keys, hence avoiding security risks. See
      # https://github.com/kubernetes-sigs/gcp-compute-persistent-disk-csi-driver/pull/610
      # for more details.
      tmpfile="$(mktemp /tmp/gcp-csi-driver-stable.XXXXXX.yaml)"
      yq eval '
         with(select(.metadata.name == "csi-gce-pd-controller" and .kind == "Deployment").spec.template.spec;
            del(.volumes[] | select(.name == "cloud-sa-volume")) |
            with(.containers[] | select(.name == "gce-pd-driver");
               del(.env[] | select(.name == "GOOGLE_APPLICATION_CREDENTIALS")) |
               del(.volumeMounts[] | select(.name == "cloud-sa-volume"))
            )
         )' "${CLUSTERLOADER_ROOT}"/drivers/gcp-csi-driver-stable.yaml > "${tmpfile}"
      kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" apply -f "${tmpfile}"
      rm "${tmpfile}"
   fi
   kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" wait -n gce-pd-csi-driver deployment csi-gce-pd-controller --for condition=available --timeout=300s

   # make sure there's a default storage class
   names=( $(kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" get sc -o name) )
   i=0
   for name in "${names[@]}"
   do
      if [[ $(kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" get $name -o jsonpath='{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}') = true ]]; then
         ((i+=1))
      fi
   done
   if [[ $i < 1 ]]; then
      kubectl --kubeconfig "${CSI_DRIVER_KUBECONFIG}" patch storageclass csi-gce-pd -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
   fi
fi

if [[ "${DEPLOY_AZURE_CSI_DRIVER:-false}" == "true" ]]; then
   curl -skSL ${AZUREDISK_CSI_DRIVER_INSTALL_URL} | bash -s ${AZUREDISK_CSI_DRIVER_VERSION} snapshot --
fi

cd "${CLUSTERLOADER_ROOT}"/ && go build -o clusterloader './cmd/'
./clusterloader --alsologtostderr --v="${CL2_VERBOSITY:-2}" "$@"
