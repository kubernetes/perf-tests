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
  export GOROOT=$ROOT_DIR/golang
}

function build_kube_cross {
  cd $ROOT_DIR/k8s.io/release
  # Note that you can't really move the tool itself around since it has
  # references to binaries that live relative to its GOROOT.
  # This is solved by copying the whole GOROOT directory below.
  cp -r $GOROOT $ROOT_DIR/k8s.io/release/images/build/cross

  cd $ROOT_DIR/k8s.io/release/images/build/cross

  # Modify Dockerfile to use previously built custom version of Go.
  # The following assumes that $GOROOT was moved to Dockerfile directory.
  sed -i 's#FROM .*$#FROM buildpack-deps:bullseye-scm\
\
COPY golang /usr/local/go\
RUN chmod -R a+rx /usr/local/go\
\
RUN export PATH="/usr/local/go/bin:$PATH"; go version\
\
ENV GOPATH /go\
ENV PATH $GOPATH\/bin:/usr/local/go/bin:$PATH\
\
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" \&\& chmod -R 777 "$GOPATH"\
WORKDIR $GOPATH#' default/Dockerfile

  make container REGISTRY=gcr.io/k8s-testimages PLATFORMS=linux/amd64 GO_MAJOR_VERSION=-master OS_CODENAME=debian

  # The make command above changes the docker buildx builder from default
  # to a custom one that uses "docker-container" driver (instead of "docker").
  # This driver forces pulling images instead of looking for them in the
  # local cache as a primary attempt. As we do not want to use any additional
  # container registry, we set the builder back to default.
  docker buildx use default
}

function build_kubernetes {
  cd $ROOT_DIR/k8s.io/kubernetes

  # Build Kubernetes using our kube-cross image.
  # Also pass GOTOOLCHAIN=local to make sure kube-build
  # uses the golang version built locally in build-go.sh.
  make quick-release \
    GOTOOLCHAIN=local \
    KUBE_CROSS_IMAGE=gcr.io/k8s-testimages/kube-cross-amd64 \
    KUBE_CROSS_VERSION=latest-go-master-debian-default
}

init
build_kube_cross
build_kubernetes
