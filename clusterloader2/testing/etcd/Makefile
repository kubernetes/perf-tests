IMAGE_NAME ?= gcr.io/k8s-staging-perf-tests/perf-tests-util/request-benchmark:latest
REQUEST_BENCHMARK_DIR ?= ../../../util-images/request-benchmark
CONFIG ?= ./config.yaml
REPORT_DIR ?= ./report
OPERATOR_MANIFEST ?= ./operator/etcd-operator.yaml
.PHONY: image load-image cluster test test-local clean clean-report

image:
	docker build -t $(IMAGE_NAME) $(REQUEST_BENCHMARK_DIR)

load-image: image
	kind load docker-image $(IMAGE_NAME)

KIND_IMAGE ?=
KIND_IMAGE_FLAG = $(if $(KIND_IMAGE),--image $(KIND_IMAGE),)

cluster:
	kind create cluster --config ./kind.yaml $(KIND_IMAGE_FLAG)
	kind export kubeconfig
	kubectl wait --for=condition=Ready nodes --all --timeout=120s

test:
	cd ../.. && \
	CL2_ENABLE_CONTAINER_RESOURCES_MEASUREMENT=true \
	CL2_PROMETHEUS_TOLERATE_MASTER=true \
	CL2_BENCHMARK_IMAGE=$(IMAGE_NAME) \
	go run ./cmd/clusterloader.go --provider kind -v=2 \
		--testconfig ./testing/etcd/$(CONFIG) \
		--kubeconfig $(HOME)/.kube/config \
		--enable-prometheus-server=true \
		--tear-down-prometheus-server=false \
		--prometheus-scrape-kube-proxy=false \
		--prometheus-apiserver-scrape-port=6443 \
		--prometheus-scrape-master-kubelets \
		--report-dir=./testing/etcd/$(REPORT_DIR)

test-local: load-image test

clean-report:
	rm -rf $(REPORT_DIR)/*

clean: clean-report
	kind delete cluster

