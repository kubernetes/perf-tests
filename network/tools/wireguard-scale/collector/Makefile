REGISTRY ?= acnpublic.azurecr.io/wireguard

POD_CREATION_IMAGE_NAME ?= wgpromcollector
POD_CREATION_VERSION ?= v0.0.5

all: build push

.PHONY: build
build:
	docker build --platform linux/amd64 -f ./Dockerfile -t $(REGISTRY)/$(POD_CREATION_IMAGE_NAME):$(POD_CREATION_VERSION) .

.PHONY: push
push: build
	docker push "$(REGISTRY)/$(POD_CREATION_IMAGE_NAME):$(POD_CREATION_VERSION)"


