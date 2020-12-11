PROJECT = k8s-testimages
IMG = gcr.io/$(PROJECT)/netperfbenchmark
TAG = 0.1

all: push

.PHONY: build
build: cmd
	docker build --pull -t $(IMG):$(TAG) .
	@echo Built $(IMG):$(TAG)

.PHONY: push
push: build
	docker push $(IMG):$(TAG)
	@echo Pushed $(IMG) with :$(TAG) tag
