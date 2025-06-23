PROJECT ?= k8s-staging-perf-tests
IMG = gcr.io/$(PROJECT)/perf-tests-util/request-benchmark
TAG = v0.0.10

all: build push

build:
	docker build --pull -t $(IMG):$(TAG) .
	docker tag $(IMG):$(TAG) $(IMG):latest
	@echo Built $(IMG):$(TAG) and tagged with latest

push: build
	docker push $(IMG):$(TAG)
	docker push $(IMG):latest
	@echo Pushed $(IMG) with :latest and :$(TAG) tags

.PHONY: build push
