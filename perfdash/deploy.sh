#!/usr/bin/env bash

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

set -euo pipefail
set -x

tmp_kube=$(mktemp -d -t "kube-$(date +%F_%H-%M-%S)_XXXX")

KUBECONFIG="$tmp_kube/config"
export KUBECONFIG

gcloud container clusters get-credentials \
	mungegithub \
	--zone us-central1-b \
	--project k8s-mungegithub \
	--verbosity=debug

kubectl apply -f "${GOPATH}/src/k8s.io/perf-tests/perfdash/deployment.yaml"
rm -rf "$tmp_kube"
