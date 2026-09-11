# Utility images

The images in this directory back the in-cluster workloads used by perf-tests, mostly
[ClusterLoader2](../clusterloader2). Each subdirectory is a self-contained image (`Dockerfile`,
sources, and a `Makefile`) that is built and pushed automatically on merge, then referenced by
the test manifests under [`clusterloader2/`](../clusterloader2).

## Changing an image

1. Make your change under `util-images/<IMAGE_NAME>/` and bump `TAG` in that image's `Makefile`,
   in the same pull request. A given tag is treated as immutable: without a bump the postsubmit
   re-pushes the same tag in place, and consumers pinned to it never pick up the change.
2. On merge, the postsubmit `post-kubernetes-push-perf-tests-<IMAGE_NAME>` builds and pushes the
   new tag to `gcr.io/k8s-staging-perf-tests/<IMAGE_NAME>` (jobs defined in
   [`config/jobs/image-pushing/k8s-staging-perf-tests.yaml`](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-perf-tests.yaml)).
3. Repoint the consumers to the new tag (for example
   [`clusterloader2/testing/list/deployment.yaml`](../clusterloader2/testing/list/deployment.yaml)).
   Pin a specific version, not `:latest`, and do not use the legacy `gcr.io/k8s-testimages`
   registry, which is no longer published to.

To test before merging, build and push to your own registry with
`make push WHAT=<IMAGE_NAME> PROJECT=<your-project>`.
