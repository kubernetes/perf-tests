#!/bin/bash

# Copyright 2021 The Kubernetes Authors.
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
set -o xtrace

# follow steps from:
# https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/
# https://kubernetes.io/docs/setup/production-environment/container-runtimes/

# enable overlay and br_netfilter so that iptables can see bridged trafic
sudo modprobe overlay
sudo modprobe br_netfilter
sudo sysctl net.bridge.bridge-nf-call-iptables=1
sudo sysctl net.bridge.bridge-nf-call-ip6tables=1
sudo sysctl net.ipv4.ip_forward=1

# update apt and install pre-reqs
sudo apt-get update
sudo apt-get install -y curl apt-transport-https

# install containerd
sudo apt-get install -y containerd
sudo mkdir -p /etc/containerd
CONFIG_TOML_PATH=/etc/containerd/config.toml
sudo bash -c "containerd config default > $CONFIG_TOML_PATH"
# apply the systemd cgroup driver to containerd
# eventually use a toml editor like jq
RUNC_OPTIONS_PATH=plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options
sudo bash -c "cat >> $CONFIG_TOML_PATH" << EOL
[$RUNC_OPTIONS_PATH]
SystemdCgroup = true
EOL
if [ "$(sudo grep "$RUNC_OPTIONS_PATH" $CONFIG_TOML_PATH | wc -l)" -ne "1" ]; then
    echo "Error: Expected 1 $RUNC_OPTIONS_PATH entry in $CONFIG_TOML_PATH"
    exit 1
fi
cat $CONFIG_TOML_PATH
sudo systemctl restart containerd
containerd --version

# install latest kubelet, kubeadm and kubectl from the official DEB packages
# this also pull other dependecies like kubernetes-cni, crictl, socat, conntrack etc.
# https://github.com/kubernetes/release/tree/master/cmd/kubepkg/templates/latest
curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add -
cat <<EOF | tee /etc/apt/sources.list.d/kubernetes.list
deb https://apt.kubernetes.io/ kubernetes-xenial main
EOF
sudo apt-get update
sudo apt-get install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

# fetch the latest binaries from CI
CI_VERSION=${CI_VERSION:-"latest"}
KUBE_VERSION=$(curl -L https://dl.k8s.io/ci/$CI_VERSION.txt)
BIN_URL="https://storage.googleapis.com/kubernetes-release-dev/ci/$KUBE_VERSION/bin/linux/amd64"
sudo curl $BIN_URL/kubelet --output /usr/bin/kubelet
sudo curl $BIN_URL/kubeadm --output /usr/bin/kubeadm
sudo curl $BIN_URL/kubectl --output /usr/bin/kubectl
kubelet --version
kubeadm version
kubectl version --client=true
