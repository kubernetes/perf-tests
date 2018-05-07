# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARCH ?= amd64
TAG ?= 1.0
REGISTRY ?= staging-k8s.gcr.io
DOCKER ?= docker

BUILD_IMAGE := kube-dns-perf-client-build
IMAGE := $(REGISTRY)/kube-dns-perf-client-$(ARCH):$(TAG)
DNSPERF_URL := ftp://ftp.nominum.com/pub/nominum/dnsperf/2.1.0.0/dnsperf-src-2.1.0.0-1.tar.gz

RUNNER_IMAGE := kube-dns-perf-runner

all: image

build/src.stamp:
	cd build/ && curl $(DNSPERF_URL) | tar -zxf -
	mv build/dnsperf-src-* build/src
	cd build/src && patch < ../../histogram.patch
	touch $@

build/build-image.stamp: Dockerfile.build
	docker build -t kube-dns-perf-client-build -f Dockerfile.build .
	touch $@

build/src/dnsperf: build/src.stamp build/build-image.stamp
	docker run --rm -v `pwd`/build/src:/src $(BUILD_IMAGE) \
		/bin/sh -c "cd /src && ./configure && make -j"

build/image.stamp: Dockerfile.image build/src/dnsperf
	docker build -t $(IMAGE) -f Dockerfile.image .
	touch $@

build/image_runner.stamp:
	cd .. && docker build -t $(RUNNER_IMAGE) -f `pwd`/image/Dockerfile.runner .
	touch $@

image: build/image.stamp
image_runner: build/image_runner.stamp

push:
	$(DOCKER) push $(IMAGE)
	$(DOCKER) push $(RUNNER_IMAGE)

clean:
	rm -rf build/*

.PHONY: clean image push
