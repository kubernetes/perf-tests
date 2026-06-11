# Copyright 2015 Google Inc. All rights reserved.
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

ARG GOLANG_VERSION=1.26.4
FROM golang:${GOLANG_VERSION} AS builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.sum go.sum
COPY go.mod go.mod
RUN go mod download

COPY *.go /workspace/
RUN CGO_ENABLED=0 go build -o $PWD/perfdash

# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/base:nonroot
# Must be 0755, not 0644: perfdash runs as nonroot (distroless:nonroot), and a
# nonroot process cannot traverse a directory without the execute bit, so every
# static asset under /www would 403 and the dashboard would fail to load.
COPY --chmod=0755 www/ /www/
COPY --from=builder /workspace/perfdash /

EXPOSE 8080
