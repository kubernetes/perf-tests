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

PACKAGE = k8s.io/perf-tests/slo-monitor
TAG = 0.12.0
# Image should be pulled from k8s.gcr.io, which will auto-detect
# region (us, eu, asia, ...) and pull from the closest.
REPOSITORY?=k8s.gcr.io
# Images should be pushed to staging-k8s.gcr.io.
PUSH_REPOSITORY?=staging-k8s.gcr.io

all: build

build: src/monitors/pod_monitor.go src/monitors/util.go src/monitors/store.go src/monitors/watcher.go src/main/slo-monitor.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -o build/slo-monitor src/main/slo-monitor.go

container: build
	docker build --pull . -t $(REPOSITORY)/slo-monitor:$(TAG)

push:
	docker tag $(REPOSITORY)/slo-monitor:$(TAG) $(PUSH_REPOSITORY)/slo-monitor:$(TAG)
	docker push $(PUSH_REPOSITORY)/slo-monitor:$(TAG)

test: src
	go test ${PACKAGE}/src/... -v

clean:
	rm -rf build/
