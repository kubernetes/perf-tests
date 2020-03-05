all: push

# See deployment.yaml for the version currently running-- bump this ahead before rebuilding!
TAG = 2.21

REPO = gcr.io/k8s-testimages
GODEP = CGO_ENABLED=0 GOOS=linux GO111MODULE=off godep

test: perfdash.go parser.go config.go downloader.go google-gcs-downloader.go
	${GODEP} go test

perfdash: test
	${GODEP} go build -a -installsuffix cgo -ldflags '-w' -o perfdash

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

deploy: push
	./deploy.sh

clean:
	rm -f perfdash
