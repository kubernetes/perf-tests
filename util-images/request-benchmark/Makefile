PROJECT ?= k8s-testimages
IMG = gcr.io/$(PROJECT)/perf-tests-util/request-benchmark
TAG = v0.0.5

all: build push

build:
	docker build --pull -t $(IMG):$(TAG) .
	docker tag $(IMG):$(TAG) $(IMG):latest
	@echo Built $(IMG):$(TAG) and tagged with latest

push:
	docker push $(IMG):$(TAG)
	docker push $(IMG):latest
	@echo Pushed $(IMG) with :latest and :$(TAG) tags

.PHONY: build push
