all: push

# See pod.yaml for the version currently running-- bump this ahead before rebuilding!
TAG = 2.9

REPO = gcr.io/k8s-testimages

perfdash: perfdash.go parser.go config.go downloader.go google-gcs-downloader.go
	CGO_ENABLED=0 GOOS=linux godep go build -a -installsuffix cgo -ldflags '-w' -o perfdash

run: perfdash
	./perfdash \
		--www \
		--address=0.0.0.0:8080 \
		--builds=20 \
		--force-builds \
		--configPath=https://raw.githubusercontent.com/kubernetes/test-infra/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml \
		--configPath=https://raw.githubusercontent.com/kubernetes/test-infra/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml \
		--configPath=https://raw.githubusercontent.com/kubernetes/test-infra/master/config/jobs/kubernetes/sig-scalability/sig-scalability-experimental-periodic-jobs.yaml


container: perfdash
	docker build --pull -t $(REPO)/perfdash:$(TAG) .

push: container
	gcloud docker -s $(REPO) -- push $(REPO)/perfdash:$(TAG)

clean:
	rm -f perfdash
