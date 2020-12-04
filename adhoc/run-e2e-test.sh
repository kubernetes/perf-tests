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

set -x

# The KUBECONFIG env variable will point to a valid kubeconfig giving you an
# admin acess to a fresh 100 node cluster. You don't have to tear the cluster
# down, it will happen automatically after the test.
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"

# If you need your test to persist any output files (e.g. test results) dump
# them into the /workspace/_artifacts. Content of this directory will get
# automatically uploaded to gcs after the test completion (successful or not).
export ARTIFACTS_DIR=/workspace/_artifacts

# KUBECTL is already set, but to a relative path (./cluster/kubectl.sh).
# Just use the normal command in PATH.
export KUBECTL=kubectl

# Check out the right branch of PMEM-CSI with support for the ad-hoc scale testing.
git clone --branch scale-testing https://github.com/pohly/pmem-csi.git
cd pmem-csi

# Reconfigure for 100 node cluster and job environment.
# TODO: let the script accept env vars...
sed -i \
    -e 's;^expected_rate=.*;expected_rate=50;' \
    -e 's;^expected_duration=.*;expected_duration=60;' \
    -e "s;^result_dir=.*;result_dir=${ARTIFACTS_DIR}/scale-test;" \
    -e 's;^GATHER_METRICS: false;GATHER_METRICS: true;' \
    -e 's;--provider=local;--provider=gce --experimental-gcp-snapshot-prometheus-disk=true --experimental-prometheus-disk-snapshot-name=po-${BUILD_ID}-${short_unique_name};' \
    hack/scale-test.sh

# Install pod vertical autoscaler.
hack/setup-va.sh

# Label all scheduleable nodes with storage=pmem.
# https://stackoverflow.com/questions/41348531/how-to-identify-schedulable-nodes-in-kubernetes/58275106#58275106
for node in $(kubectl get no -o 'go-template={{range .items}}{{$taints:=""}}{{range .spec.taints}}{{if eq .effect "NoSchedule"}}{{$taints = print $taints .key ","}}{{end}}{{end}}{{if not $taints}}{{.metadata.name}}{{ "\n"}}{{end}}{{end}}'); do
    kubectl label node/$node storage=pmem
done

# Ready..
hack/scale-test.sh

exit 0
