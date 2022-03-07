REGISTRY ?= gcr.io/k8s-staging-perf-tests
IMAGE_NAME = dnsperfgo
VERSION ?= v1.3.0

all: push

.PHONY: build_bin
build_bin:
	CGO_ENABLED=1 go build -o dnsperfcgo
	CGO_ENABLED=0 go build -o dnsperfgo

.PHONY: build
build: build_bin
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(VERSION) .

.PHONY: push
push: build
	docker push "$(REGISTRY)/$(IMAGE_NAME):$(VERSION)"
