#!/bin/bash
# Copyright 2017 The Kubernetes Authors.
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

# run the script to run e2e tests for cluster loader, netperf and dns. 
# This enables the same to be invoked from kubetest from test-infra.

set -o nounset
set -o pipefail

PERFTEST_ROOT=$(dirname "${BASH_SOURCE}")
#CLUSTERLOADER
cd ${PERFTEST_ROOT}/clusterloader/e2e/ && go test -c -o e2e.test
./e2e.test --ginkgo.v=true --ginkgo.focus="Cluster\sLoader" --kubeconfig="${HOME}/.kube/config" --viper-config=../config/test
#NETPERF
cd ${PERFTEST_ROOT}/network/benchmarks/netperf/ && go run ./launch.go  --kubeConfig="${HOME}/.kube/config" --hostnetworking --iterations 1
#DNS
cd ${PERFTEST_ROOT}/dns
 ./run --params params/kubedns/default.yaml --out-dir out --use-cluster-dns
