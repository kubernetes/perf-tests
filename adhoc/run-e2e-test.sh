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

# The KUBECONFIG env variable will point to a valid kubeconfig giving you an
# admin acess to a fresh 100 node cluster. You don't have to tear the cluster
# down, it will happen automatically after the test.
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/config}"

# If you need your test to persist any output files (e.g. test results) dump
# them into the /workspace/_artifacts. Content of this directory will get
# automatically uploaded to gcs after the test completion (successful or not).
export ARTIFACTS_DIR=/workspace/_artifacts


# Implement ad-hoc test here, e.g. install extra addons via kubectl then run
# clusterloader2 with your custom config.

kubectl get nodes > ${ARTIFACTS_DIR}/node-list.txt

exit 0
