FROM golang:1.20 AS build-env

ARG gopkg=k8s.io/perf-tests/util-images/watch-list

ADD [".", "/go/src/$gopkg"]

WORKDIR /go/src/$gopkg
RUN CGO_ENABLED=0 go build -o /go/bin/watch-list main.go

FROM gcr.io/distroless/static
COPY --from=build-env /go/bin/watch-list /usr/bin/
ENTRYPOINT ["watch-list"]
