FROM golang:1.9 AS builder

WORKDIR /opt/apps/nptests
ADD ./nptest.go nptest.go
RUN go build -o nptests ./nptest.go

FROM debian:jessie

RUN apt-get update \
    && apt-get install -y iperf3 curl wget net-tools gcc make \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /tmp
WORKDIR /tmp
# Download and build netperf from sources
RUN curl -LO https://github.com/HewlettPackard/netperf/archive/netperf-2.7.0.tar.gz
RUN tar -xzf netperf-2.7.0.tar.gz
RUN cd netperf-netperf-2.7.0 && ./configure --prefix=/usr/local --bindir /usr/local/bin && make && make install

COPY --from=builder /opt/apps/nptests/nptests /usr/bin/

ENTRYPOINT ["nptests"]
