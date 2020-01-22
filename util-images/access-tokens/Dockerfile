FROM golang:1.13.4 AS build-env

ARG gopkg=k8s.io/perf-tests/util-images/access-tokens

ADD ["cmd", "/go/src/$gopkg/cmd"]
ADD ["go.mod", "/go/src/$gopkg"]
ADD ["go.sum", "/go/src/$gopkg"]

WORKDIR /go/src/$gopkg
RUN CGO_ENABLED=0 go build -o /workspace/access-tokens ./cmd

FROM scratch
WORKDIR /
COPY --from=build-env /workspace/access-tokens /access-tokens
ENTRYPOINT ["/access-tokens"]

