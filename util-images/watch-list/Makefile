PROJECT = k8s-staging-perf-tests
IMG = gcr.io/$(PROJECT)/watch-list
TAG = v0.0.1

all: push

.PHONY: build
build:
	docker build --pull -t $(IMG):$(TAG) .
	docker tag $(IMG):$(TAG) $(IMG):latest
	@echo Built $(IMG):$(TAG) and tagged with latest

.PHONY: push
push: build
	docker push $(IMG):$(TAG)
	docker push $(IMG):latest
	@echo Pushed $(IMG) with :latest and :$(TAG) tags
