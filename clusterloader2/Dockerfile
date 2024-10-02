ARG GOLANG_VERSION=1.22
FROM golang:${GOLANG_VERSION} AS builder

WORKDIR /root/perf-tests/clusterloader2
COPY . .
RUN go build -o clusterloader2 ./cmd/

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl unzip
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" \
    && unzip awscliv2.zip \
    && ./aws/install \
    && rm -rf awscliv2.zip aws
COPY --from=builder /root/perf-tests /root/perf-tests
RUN mkdir $GOPATH/src/k8s.io -p && \
    cp /root/perf-tests $GOPATH/src/k8s.io/perf-tests -r
WORKDIR /root/perf-tests/clusterloader2
ENTRYPOINT ["./clusterloader2"]
