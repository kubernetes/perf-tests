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
TAG = 0.9.2
REPOSITORY = google-containers

all: build

deps:
	go get -u github.com/tools/godep

build: src/monitors/pod_monitor.go src/monitors/util.go src/monitors/store.go src/monitors/watcher.go src/main/slo-monitor.go deps
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 godep go build -a -o build/slo-monitor src/main/slo-monitor.go

push:
	sudo docker build . -t gcr.io/$(REPOSITORY)/slo-monitor:$(TAG)
	gcloud docker -- push gcr.io/$(REPOSITORY)/slo-monitor:$(TAG)

test: src
	godep go test ${PACKAGE}/src -v

clean:
	rm -f build/slo-monitor
