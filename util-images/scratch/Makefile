PROJECT ?= k8s-staging-perf-tests
IMG = gcr.io/$(PROJECT)/perf-tests-util/scratch
NUM_IMAGES ?= 20

SHELL := /bin/bash

all: push

build: Dockerfile
	TAG=1 ; while [[ $$TAG -le $(NUM_IMAGES) ]] ; do \
		touch $$TAG ; \
		docker build --build-arg FILENAME=$$TAG -t $(IMG):$$TAG . ; \
		rm $$TAG ; \
		echo Built $(IMG):$$TAG ; \
		((TAG = TAG + 1)) ; \
	done

.PHONY: push
push: build
	TAG=1 ; while [[ $$TAG -le $(NUM_IMAGES) ]] ; do \
		docker push $(IMG):$$TAG ; \
		echo Pushed $(IMG) with :$$TAG tags ; \
		((TAG = TAG + 1)) ; \
	done
