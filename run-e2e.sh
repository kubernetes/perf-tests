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
echo "TOOL_NAME: $1"

case "$1" in
  cluster-loader2 )
    #CLUSTERLOADER2
    echo "COMMAND: ${PERFTEST_ROOT}/clusterloader2 && ./run-e2e.sh ${@:2}"
    cd ${PERFTEST_ROOT}/clusterloader2 && ./run-e2e.sh ${@:2}
    exit
    ;;
  network-performance )
    #NETPERF
    cd ${PERFTEST_ROOT}/network/benchmarks/netperf/ && go run ./launch.go  --kubeConfig="${HOME}/.kube/config" --hostnetworking --iterations 1
    exit
    ;;
  kube-dns|core-dns|node-local-dns )
    cd ${PERFTEST_ROOT}/dns
    ./run $@
    exit
    ;;
  --help | -h )
    echo  " cluster-loader2               Run Cluster Loader 2 Test"
    echo  " network-performance           Run Network Performance Test"
    echo  " kube-dns                      Run Kube-DNS test"
    echo  " core-dns                      Run Core-DNS test"
    echo  " node-local-dns                Run NodeLocalDNS test"
    exit
    ;;
esac
