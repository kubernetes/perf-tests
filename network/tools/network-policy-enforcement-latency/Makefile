REGISTRY ?= gcr.io/k8s-staging-perf-tests/network-policy-enforcement-latency

POD_CREATION_IMAGE_NAME ?= pod-creation-reachability-latency
POD_CREATION_PATH ?= $(POD_CREATION_IMAGE_NAME)
POD_CREATION_VERSION ?= v0.0.1

POLICY_CREATION_IMAGE_NAME ?= policy-creation-enforcement-latency
POLICY_CREATION_PATH ?= $(POLICY_CREATION_IMAGE_NAME)
POLICY_CREATION_VERSION ?= v0.0.1

all: push_pod_creation push_policy_creation

.PHONY: build_pod_creation
build_pod_creation:
	docker build -f ./$(POD_CREATION_PATH)/Dockerfile -t $(REGISTRY)/$(POD_CREATION_IMAGE_NAME):$(POD_CREATION_VERSION) .

.PHONY: push_pod_creation
push_pod_creation: build_pod_creation
	docker push "$(REGISTRY)/$(POD_CREATION_IMAGE_NAME):$(POD_CREATION_VERSION)"

.PHONY: build_policy_creation
build_policy_creation:
	docker build -f ./$(POLICY_CREATION_PATH)/Dockerfile -t $(REGISTRY)/$(POLICY_CREATION_IMAGE_NAME):$(POLICY_CREATION_VERSION) .

.PHONY: push_policy_creation
push_policy_creation: build_policy_creation
	docker push "$(REGISTRY)/$(POLICY_CREATION_IMAGE_NAME):$(POLICY_CREATION_VERSION)"
