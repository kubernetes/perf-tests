all: push

# See deployment.yaml for the version currently running-- bump this ahead before rebuilding!
TAG?=2.56

REPO?=gcr.io/k8s-staging-perf-tests

.PHONY: test
test: perfdash.go parser.go config.go metrics-downloader-helper.go metrics-downloader.go gcs_metrics_bucket.go s3_metrics_bucket.go local_metrics_dir.go
	go test

perfdash: test
	go build -a -installsuffix cgo -ldflags '-w' -o perfdash

.PHONY: run
run: perfdash
	./perfdash \
		--www \
		--address=0.0.0.0:8080 \
		--builds=20 \
		--force-builds \
		--configPath=test/jobs.yaml

# Use buildkit to have "COPY --chmod=" support (availability of it in "regular" docker build depends on docker version).
.PHONY: container
container:
	DOCKER_BUILDKIT=1 docker build --pull -t $(REPO)/perfdash:$(TAG) .

.PHONY: push
push: container
	gcloud docker -s $(REPO) -- push $(REPO)/perfdash:$(TAG)

.PHONY: clean
clean:
	rm -f perfdash
