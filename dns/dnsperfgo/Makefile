REGISTRY ?= gcr.io/k8s-staging-perf-tests
IMAGE_NAME = dnsperfgo
VERSION ?= v1.3.0

all: push


.PHONY: build
build: 
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(VERSION) .

.PHONY: push
push: build
	docker push "$(REGISTRY)/$(IMAGE_NAME):$(VERSION)"
