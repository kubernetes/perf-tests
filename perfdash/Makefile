all: push

# See deployment.yaml for the version currently running-- bump this ahead before rebuilding!
TAG?=2.44

REPO?=gcr.io/k8s-staging-perf-tests

test: perfdash.go parser.go config.go metrics-downloader-helper.go metrics-downloader.go gcs_metrics_bucket.go s3_metrics_bucket.go
	go test

perfdash: test
	go build -a -installsuffix cgo -ldflags '-w' -o perfdash

run: perfdash
	./perfdash \
		--www \
		--address=0.0.0.0:8080 \
		--builds=20 \
		--force-builds \
		--githubConfigDir=https://api.github.com/repos/kubernetes/perf-tests/contents/perfdash/test

container:
	docker buildx create --use
	docker buildx build --load -t $(REPO)/perfdash:$(TAG) .

push: container
	gcloud auth configure-docker
	docker buildx create --use
	docker buildx build --push -t $(REPO)/perfdash:$(TAG) --platform "linux/amd64,linux/arm64" .

clean:
	rm -f perfdash
