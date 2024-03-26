FROM golang:1.22 AS build-env

ARG gopkg=k8s.io/perf-tests/util-images/sleep

ADD [".", "/go/src/$gopkg"]

WORKDIR /go/src/$gopkg
RUN CGO_ENABLED=0 go build -o /go/bin/sleep main.go

FROM gcr.io/distroless/static
COPY --from=build-env /go/bin/sleep /usr/bin/
ENTRYPOINT ["sleep"]
