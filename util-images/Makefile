ifndef WHAT
$(error WHAT is not set. Please set it before running make.)
endif

all: push

.PHONY: build
build:
	make -C $(WHAT) build

.PHONY: push
push: build
	make -C $(WHAT) push
