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

# Build the application
FROM golang:1.14-buster as build

RUN go get -u github.com/tools/godep
WORKDIR /go/src/app
COPY . /go/src/app
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=off godep go test
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=off godep go build -a -installsuffix cgo -ldflags '-w' -o perfdash

# Copy files into run image
FROM gcr.io/distroless/base-debian10
WORKDIR /app
COPY --from=build /go/src/app/perfdash perfdash
COPY www/ www
ENTRYPOINT ["/app/perfdash"]