FROM alpine:3.7

RUN \
	apk update && \
	apk add bind-libs bind-tools libcap libcrypto1.0

COPY build/src/dnsperf /dnsperf
COPY build/src/resperf /resperf
