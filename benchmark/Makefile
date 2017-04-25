OUT_DIR := build
SOURCES := $(shell find pkg -name '*.go' | grep -v "_test.go")
TESTS := $(shell find pkg -name '*_test.go')

all: benchmark

deps:
	go get -u github.com/tools/godep

benchmark: deps cmd/benchmark.go $(SOURCES)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 godep go build -a -o $(OUT_DIR)/benchmark cmd/benchmark.go

test: deps $(TESTS)
	godep go test k8s.io/perf-tests/benchmark/pkg/... -v

.PHONY: all benchmark clean test
