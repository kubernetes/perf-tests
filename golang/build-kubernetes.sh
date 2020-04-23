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

set -euxo pipefail

# Initialize necessary environment variables.
function init {
  cd go
  export GOROOT=/go/src/k8s.io/perf-tests/golang/go
  export TAG=$(date +v%Y%m%d)-$(git rev-parse --short HEAD)
  cd ..
}

function clone_release {
  git clone https://github.com/kubernetes/release.git /go/src/k8s.io/release
  # Note that you can't really move the tool itself around since it has
  # references to binaries that live relative to its GOROOT.
  # This is solved by copying the whole GOROOT directory below.
  cp -r $GOROOT /go/src/k8s.io/release/images/build/cross
}

function build_kube_cross {
  cd /go/src/k8s.io/release/images/build/cross

  # Modify Dockerfile to use previously built custom version.
  # The following assumes that $GOROOT was moved to Dockerfile directory.
  sed -i 's#FROM golang.*$#FROM buildpack-deps:stretch-scm\
\
COPY go /usr/local/go\
RUN chmod -R a+rx /usr/local/go\
\
RUN export PATH="/usr/local/go/bin:$PATH"; go version\
\
ENV GOPATH /go\
ENV PATH $GOPATH\/bin:/usr/local/go/bin:$PATH\
\
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" \&\& chmod -R 777 "$GOPATH"\
WORKDIR $GOPATH#' Dockerfile

  # Set VERSION contents to the tag of kube-cross Docker image.
  sed -i 's#.*#'"$TAG"'#' VERSION

  REGISTRY=gcr.io/k8s-testimages
  STAGING_REGISTRY=$REGISTRY PROD_REGISTRY=$REGISTRY TAG=$TAG make build
}

function build_kubernetes {
  git clone https://github.com/kubernetes/kubernetes.git /go/src/k8s.io/kubernetes
  cd /go/src/k8s.io/kubernetes/build/build-image
  git checkout $K8S_COMMIT

  # Change the base image of kube-build to our own kube-cross image.
  sed -i 's#FROM .*#FROM gcr.io/k8s-testimages/kube-cross-amd64:'"$TAG"'#' Dockerfile

  cd /go/src/k8s.io/kubernetes
  # Commit changes - needed to not create a "dirty" build, so we can push the
  # build to <bucket>/ci directory and update latest.txt file.
  git config user.email "scalability@k8s.io"
  git config user.name "k8s scalability"
  git commit -am "Upgrade cross Dockerfile to use kube-cross with newest golang"
  # Build Kubernetes using our kube-cross image.
  make quick-release
}

init
clone_release
build_kube_cross
build_kubernetes
