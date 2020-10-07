FROM golang:buster AS build-env
ARG gopkg=k8s.io/perf-tests/util-images/phases/netperfbenchmark

ADD [".", "/go/src/$gopkg"]

#ENV GO111MODULE on
WORKDIR /go/src/$gopkg
RUN CGO_ENABLED=0 go build -o /workspace/phases ./cmd

FROM golang:buster
ENV LD_LIBRARY_PATH=/usr/local/lib

RUN apt-get update \
    && apt-get install -y  curl wget net-tools gcc g++ make \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /tmp

# Download and build iperf3 from sources
RUN curl -LO https://downloads.es.net/pub/iperf/iperf-3.1.tar.gz && tar zxf iperf-3.1.tar.gz
RUN rm -rf iperf-3.1.tar.gz
RUN cd iperf-3.1 && ./configure --prefix=/usr/local --bindir /usr/local/bin && make && make install

RUN curl -LO https://iperf.fr/download/source/iperf-2.0.9-source.tar.gz && tar zxf iperf-2.0.9-source.tar.gz
RUN rm -rf iperf-2.0.9-source.tar.gz
RUN cd iperf-2.0.9 && ./configure --prefix=/usr/local --bindir /usr/local/bin && make && make install


ENV VERSION=3.1.4

RUN curl -LO http://download.joedog.org/siege/siege-$VERSION.tar.gz > siege-$VERSION.tar.gz && tar -xf siege-${VERSION}.tar.gz
RUN rm -rf siege-${VERSION}.tar.gz
RUN cd siege-${VERSION} && ./configure &&  make install

#RUN apt-get update \
#    && apk add curl g++ make \
#    && curl http://download.joedog.org/siege/siege-$VERSION.tar.gz > siege-$VERSION.tar.gz \
#    && tar -xf siege-${VERSION}.tar.gz \
#    && cd siege-${VERSION} \
#    && ./configure \
#    && make install

WORKDIR /workspace
COPY --from=build-env /workspace/phases .
ENTRYPOINT ["/workspace/phases"]
