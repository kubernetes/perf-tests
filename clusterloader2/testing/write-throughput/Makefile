IMAGE_NAME ?= gcr.io/k8s-staging-perf-tests/perf-tests-util/request-benchmark:latest
REQUEST_BENCHMARK_DIR ?= ../../../util-images/request-benchmark
CLUSTER_NAME ?= kind
CONFIG ?= ./config.yaml
REPORT_DIR ?= ./report

.PHONY: image load-image cluster test test-local clean

image:
	docker build -t $(IMAGE_NAME) $(REQUEST_BENCHMARK_DIR)

load-image: image
	kind load docker-image $(IMAGE_NAME) --name $(CLUSTER_NAME)

KIND_IMAGE ?=
KIND_IMAGE_FLAG = $(if $(KIND_IMAGE),--image $(KIND_IMAGE),)

cluster:
	kind create cluster --config ./kind.yaml --name $(CLUSTER_NAME) $(KIND_IMAGE_FLAG)
	kind export kubeconfig --name $(CLUSTER_NAME)
	kubectl wait --for=condition=Ready nodes --all --timeout=60s

test:
	cd ../.. && \
	CL2_ENABLE_CONTAINER_RESOURCES_MEASUREMENT=true \
	CL2_PROMETHEUS_TOLERATE_MASTER=true \
	go run ./cmd/clusterloader.go --provider kind -v=2 \
		--testconfig ./testing/write-throughput/$(CONFIG) \
		--kubeconfig $(HOME)/.kube/config \
		--enable-prometheus-server=true \
		--tear-down-prometheus-server=false \
		--prometheus-scrape-kube-proxy=false \
		--prometheus-apiserver-scrape-port=6443 \
		--prometheus-scrape-master-kubelets \
		--prometheus-scrape-etcd=true \
		--report-dir=./testing/write-throughput/$(REPORT_DIR)


test-local: load-image test


clean:
	kind delete cluster --name $(CLUSTER_NAME)
	rm -rf $(REPORT_DIR)/*
