# containerd

Utility image used by the CL2 image preloader. Uses containerd instead of docker.

## Testing and Usage

1. Build an image with `PROJECT=<TEST-PROJECT> make build`
1. Apply image preload daemonset to your cluster (hardcode the image to pull)
    * `kubectl apply -f ~/go/src/k8s.io/perf-tests/clusterloader2/pkg/imagepreload/manifests/daemonset.yaml`

## Releasing

1. If required, test with steps from `Testing and Usage`
1. Increment the `TAG` in the Makefile
1. Build with `make build`
1. Release with `make push`
