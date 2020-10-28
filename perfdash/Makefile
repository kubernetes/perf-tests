all: push

# See deployment.yaml for the version currently running-- bump this ahead before rebuilding!
TAG = 2.31

REPO = gcr.io/k8s-testimages

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
		--githubConfigDir=https://api.github.com/repos/kubernetes/test-infra/contents/config/jobs/kubernetes/sig-scalability \
		--githubConfigDir=https://api.github.com/repos/kubernetes/test-infra/contents/config/jobs/kubernetes/sig-release/release-branch-jobs

container: perfdash
	docker build --pull -t $(REPO)/perfdash:$(TAG) .

push: container
	gcloud docker -s $(REPO) -- push $(REPO)/perfdash:$(TAG)

clean:
	rm -f perfdash

