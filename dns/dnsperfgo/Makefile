PROJECT := $(shell gcloud config get-value project)
REGISTRY ?= gcr.io/${PROJECT}

IMAGE_NAME ?= dnsperfgo

VERSION ?= $(shell git describe --tags --always --dirty)

ifneq (,$(findstring dirty,$(VERSION)))
        VERSION := latest
endif

build_bin:
			CGO_ENABLED=1 go build -o dnsperfcgo
			CGO_ENABLED=0 go build -o dnsperfgo

build:	build_bin
		docker build -t $(REGISTRY)/$(IMAGE_NAME):$(VERSION) .


push:	build
		docker push "$(REGISTRY)/$(IMAGE_NAME):$(VERSION)"
