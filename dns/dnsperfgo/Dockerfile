# build image
FROM golang:1.21 AS build-env
ARG gopkg=k8s.io/perf-tests/dns/dnsperfgo
ADD [".", "/go/src/$gopkg"]
WORKDIR /go/src/$gopkg

RUN CGO_ENABLED=1 go build -o /dnsperfcgo
RUN CGO_ENABLED=0 go build -o /dnsperfgo

# runtime image
FROM alpine:3.10
# install bind-tools to prevent nslookup from sending PTR queries for each response IP
RUN apk add --no-cache ca-certificates bash tcpdump git bind-tools
# This is to make sure dnstest compiled on debian can run in alpine/musl
RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2
COPY --from=build-env /dnsperfgo /dnsperfgo
COPY --from=build-env /dnsperfcgo /dnsperfcgo
ENTRYPOINT ["/dnsperfcgo"]
