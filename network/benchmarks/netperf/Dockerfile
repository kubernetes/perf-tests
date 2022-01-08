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

# network performance tests in containers
#
# Run as the Orchestrator: Arguments: --mode=orchestrator
#
# Run as a Worker (first get orchestrator virtual IP address):
# kubectl get svc --format "{{ .NetworkSettings.IPAddress }}" netperf-orch
#
# Args: --mode=worker --host=<service cluster ip> --port=5202
#
# Compile the application first (stage 1)
FROM golang:1.17-bullseye as gobuilder

WORKDIR /src

COPY nptest/go.mod .
RUN go mod download

COPY nptest/nptest.go .

# Build executable
RUN GOOS=linux go build -o nptests .

# Start from a fresh image so we can only tranfer over the binary (stage 2)
FROM debian:bullseye-slim

# Install CA certs and useful tools
RUN set -eux; \
    sed -i 's/ main$/ main contrib non-free/g' /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl wget iperf3 netperf; \
    rm -rf /var/lib/apt/lists/*

COPY --from=gobuilder /src/nptests /usr/bin/

ENTRYPOINT ["nptests"]
