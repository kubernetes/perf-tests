FROM golang:1.12.1 AS build-env

ARG gopkg=k8s.io/perf-tests/util-images/probes

ADD [".", "/go/src/$gopkg"]

ENV GO111MODULE on
WORKDIR /go/src/$gopkg
RUN CGO_ENABLED=0 go build -o /workspace/probes ./cmd

FROM golang:1.12.1-alpine
WORKDIR /workspace
COPY --from=build-env /workspace/probes .
ENTRYPOINT ["/workspace/probes"]
