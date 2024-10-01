DOCKERFILE = Dockerfile
IMAGE_REPO ?= localhost:5000
IMAGE_TAG ?= latest
IMAGE_NAME = $(IMAGE_REPO)/clusterloader2:$(IMAGE_TAG)

PHONY: build-image push-image all

all: build-image push-image

build-image:
	docker build -t $(IMAGE_NAME) -f $(DOCKERFILE) .

push-image:
	docker push $(IMAGE_NAME)