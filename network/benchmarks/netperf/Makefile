# Copyright 2016 The Kubernetes Authors All rights reserved.
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

all: docker nptests push launch runtests

DOCKERREPO       := girishkalele/netperf-latest

# build with buildx
PLATFORMS?=linux/amd64,linux/arm64
OUTPUT=

docker: launch ensure-buildx
	docker buildx build --platform=${PLATFORMS} $(OUTPUT) -t $(DOCKERREPO) --pull .

# push the cross built image
push: OUTPUT=--push
push: docker

nptests: nptest/nptest.go
	cd nptest; GOOS=linux make nptests

clean:
	@rm -f Dockerbuild/*
	@rm -f ./nptests ./*svg ./*png ./*jpg ./*csv ./*.cap ./*.pyc nptests ./*~
	@go clean
	cd nptest; make clean

launch: launch.go
	go build -o launch launch.go

# 'runtests' is the test runner target
runtests: launch
	@echo Launching network performance tests
	./launch
	@echo Network performance tests complete - running matplotlib to generate graphs and copy them back
	cp netperf-latest.csv plotperf && cd plotperf; make plot && mv *png .. && mv *svg ..
	@echo Results file netperf-latest.csv and SVG/PNG graphs generated successfully

# enable buildx
DOCKER_CLI_EXPERIMENTAL=enabled
ensure-buildx:
	./../../../hack/build/init-buildx.sh
